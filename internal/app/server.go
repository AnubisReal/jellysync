package app

import (
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	webassets "github.com/AnubisReal/jellysync/web"
)

type server struct {
	logger    *slog.Logger
	store     *configStore
	transfers *transferManager
	sessions  *sessionStore
}

func Run(logger *slog.Logger) error {
	addr := env("JELLYSYNC_ADDR", ":8090")
	dataDir := env("JELLYSYNC_DATA_DIR", "./data")
	store, err := newConfigStore(dataDir)
	if err != nil {
		return err
	}
	transfers, err := newTransferManager(store, logger)
	if err != nil {
		return err
	}
	s := &server{logger: logger, store: store, transfers: transfers, sessions: newSessionStore()}
	web, err := fs.Sub(webassets.Files, "static")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /api/v1/config", s.getConfig)
	mux.HandleFunc("POST /api/v1/setup", s.setup)
	mux.HandleFunc("POST /api/v1/login", s.login)
	mux.HandleFunc("POST /api/v1/claim", s.claim)
	protected := http.NewServeMux()
	protected.HandleFunc("PUT /api/v1/jellyfin", s.updateJellyfin)
	protected.HandleFunc("GET /api/v1/discover", s.discover)
	protected.HandleFunc("GET /api/v1/images/{source}/{id}", s.image)
	protected.HandleFunc("GET /api/v1/downloads", s.listDownloads)
	protected.HandleFunc("POST /api/v1/downloads", s.createDownload)
	protected.HandleFunc("PUT /api/v1/network/reconnect", s.reconnectNetwork)
	protected.HandleFunc("DELETE /api/v1/peers/{id}", s.deletePeer)
	protected.HandleFunc("GET /api/v1/storage", s.getStorage)
	protected.HandleFunc("PUT /api/v1/storage", s.updateStorage)
	protected.HandleFunc("GET /api/v1/storage/browse", s.browseStorage)
	mux.Handle("/api/v1/", s.requireAdmin(protected))
	mux.HandleFunc("POST /peer/v1/register", s.peerRegister)
	mux.HandleFunc("GET /peer/v1/catalog", s.peerCatalog)
	mux.HandleFunc("GET /peer/v1/item/{id}", s.peerItem)
	mux.HandleFunc("GET /peer/v1/images/{id}", s.peerImage)
	mux.HandleFunc("GET /peer/v1/download/{id}", s.peerDownload)
	mux.Handle("/", spaHandler(web))

	httpServer := &http.Server{
		Addr: addr, Handler: securityHeaders(mux), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 0, IdleTimeout: 60 * time.Second,
	}
	logger.Info("jellysync ready", "address", addr)
	return httpServer.ListenAndServe()
}

func spaHandler(web fs.FS) http.Handler {
	files := http.FileServer(http.FS(web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if requested != "." && requested != "" {
			if info, err := fs.Stat(web, requested); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
			if path.Ext(requested) != "" {
				http.NotFound(w, r)
				return
			}
		}
		clone := r.Clone(r.Context())
		clone.URL.Path = "/"
		files.ServeHTTP(w, clone)
	})
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.get()
	if !cfg.Configured {
		writeJSON(w, http.StatusOK, PublicConfig{})
		return
	}
	if cfg.AdminHash == "" {
		writeJSON(w, http.StatusOK, PublicConfig{Configured: true, NeedsClaim: true})
		return
	}
	if !s.sessions.valid(r) {
		writeJSON(w, http.StatusOK, PublicConfig{Configured: true})
		return
	}
	public := s.store.public(true)
	public.Authenticated = true
	writeJSON(w, http.StatusOK, public)
}

func (s *server) setup(w http.ResponseWriter, r *http.Request) {
	previous := s.store.get()
	if previous.Configured {
		writeError(w, http.StatusConflict, "Esta instancia ya está configurada.")
		return
	}
	var cfg Config
	if err := decodeJSON(w, r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, "La configuración enviada no es válida.")
		return
	}
	libraries, err := fetchJellyfinLibraries(cfg.JellyfinURL, cfg.JellyfinAPIKey)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	cfg.Libraries = libraries
	if err := s.store.save(cfg); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	saved := s.store.get()
	if saved.Mode == "node" {
		coordinator, err := registerWithCoordinator(saved)
		if err != nil {
			_ = s.store.write(previous)
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		if err := s.store.addPeer(coordinator); err != nil {
			_ = s.store.write(previous)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.logger.Info("node configured", "mode", saved.Mode, "node", saved.NodeName)
	s.sessions.create(w, r)
	public := s.store.public(true)
	public.Authenticated = true
	writeJSON(w, http.StatusCreated, public)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password string `json:"password"`
	}
	if decodeJSON(w, r, &input) != nil || !passwordMatches(input.Password, s.store.get()) {
		writeError(w, http.StatusUnauthorized, "La contraseña no es correcta.")
		return
	}
	s.sessions.create(w, r)
	public := s.store.public(true)
	public.Authenticated = true
	writeJSON(w, http.StatusOK, public)
}

func (s *server) claim(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "La contraseña no es válida.")
		return
	}
	if err := s.store.setAdminPassword(input.Password); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.sessions.create(w, r)
	public := s.store.public(true)
	public.Authenticated = true
	writeJSON(w, http.StatusOK, public)
}

func (s *server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.sessions.valid(r) {
			writeError(w, http.StatusUnauthorized, "Debes iniciar sesión.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) updateJellyfin(w http.ResponseWriter, r *http.Request) {
	var credentials jellyfinCredentials
	if err := decodeJSON(w, r, &credentials); err != nil {
		writeError(w, http.StatusBadRequest, "Los datos de Jellyfin no son válidos.")
		return
	}
	if credentials.APIKey == "" {
		credentials.APIKey = s.store.get().JellyfinAPIKey
	}
	libraries, err := fetchJellyfinLibraries(credentials.URL, credentials.APIKey)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := s.store.updateJellyfin(credentials.URL, credentials.APIKey, libraries); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.store.public(true))
}

type discoverResponse struct {
	Items   []CatalogItem `json:"items"`
	Sources []Peer        `json:"sources"`
	Errors  []string      `json:"errors,omitempty"`
}

func (s *server) discover(w http.ResponseWriter, _ *http.Request) {
	cfg := s.store.get()
	response := discoverResponse{Sources: []Peer{{ID: cfg.NodeID, Name: cfg.NodeName, URL: cfg.PublicURL}}}
	items, err := fetchCatalog(cfg)
	if err != nil {
		response.Errors = append(response.Errors, err.Error())
	} else {
		response.Items = append(response.Items, items...)
	}
	for _, peer := range cfg.Peers {
		response.Sources = append(response.Sources, peer)
		items, err := fetchPeerCatalog(peer, cfg.NetworkKey)
		if err != nil {
			response.Errors = append(response.Errors, err.Error())
			continue
		}
		response.Items = append(response.Items, items...)
	}
	slices.SortFunc(response.Items, func(a, b CatalogItem) int { return b.DateCreated.Compare(a.DateCreated) })
	writeJSON(w, http.StatusOK, response)
}

func (s *server) image(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.get()
	source, itemID := r.PathValue("source"), r.PathValue("id")
	if source == cfg.NodeID {
		proxyJellyfinImage(w, cfg, itemID)
		return
	}
	peer, ok := findPeer(cfg.Peers, source)
	if !ok {
		http.Error(w, "nodo desconocido", http.StatusNotFound)
		return
	}
	response, err := requestPeer(peer, cfg.NetworkKey, peerPath("images", itemID))
	if err != nil {
		http.Error(w, "imagen no disponible", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		http.Error(w, "imagen no disponible", response.StatusCode)
		return
	}
	w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = io.Copy(w, io.LimitReader(response.Body, 12<<20))
}

func (s *server) listDownloads(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.transfers.list())
}

func (s *server) createDownload(w http.ResponseWriter, r *http.Request) {
	var input downloadRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "La solicitud de descarga no es válida.")
		return
	}
	cfg := s.store.get()
	if input.SourceID == cfg.NodeID {
		writeError(w, http.StatusConflict, "El contenido ya pertenece a este servidor.")
		return
	}
	peer, ok := findPeer(cfg.Peers, input.SourceID)
	if !ok {
		writeError(w, http.StatusNotFound, "El servidor de origen no está conectado.")
		return
	}
	job := s.transfers.start(peer, input.ItemID)
	writeJSON(w, http.StatusAccepted, job)
}

func (s *server) reconnectNetwork(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Coordinator string `json:"coordinator"`
		NetworkKey  string `json:"networkKey"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Los datos de conexión no son válidos.")
		return
	}
	candidate, err := s.store.reconnectNode(input.Coordinator, input.NetworkKey)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	coordinator, err := registerWithCoordinator(candidate)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	candidate.Peers = []Peer{coordinator}
	if err := s.store.write(candidate); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.store.public(true))
}

func (s *server) deletePeer(w http.ResponseWriter, r *http.Request) {
	if s.store.get().Mode != "coordinator" {
		writeError(w, http.StatusForbidden, "Solo el coordinador puede eliminar servidores.")
		return
	}
	if err := s.store.removePeer(r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.store.public(true))
}

func (s *server) getStorage(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, currentStoragePaths(s.store.get()))
}

func (s *server) updateStorage(w http.ResponseWriter, r *http.Request) {
	var input StoragePaths
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Las rutas enviadas no son válidas.")
		return
	}
	paths, err := validateStoragePaths(input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := s.store.updateStorage(paths); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, paths)
}

func (s *server) browseStorage(w http.ResponseWriter, r *http.Request) {
	requested := r.URL.Query().Get("path")
	if requested == "" {
		requested = storageRoot()
	}
	listing, err := browseDirectories(requested)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

func (s *server) peerRegister(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.get()
	if cfg.Mode != "coordinator" || !peerAuthorized(r, cfg.NetworkKey) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}
	var registration peerRegistration
	if err := decodeJSON(w, r, &registration); err != nil {
		http.Error(w, "registro no válido", http.StatusBadRequest)
		return
	}
	if err := s.store.addPeer(Peer(registration)); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, http.StatusCreated, Peer{ID: cfg.NodeID, Name: cfg.NodeName, URL: cfg.PublicURL})
}

func (s *server) peerCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.authorizePeer(w, r) {
		return
	}
	items, err := fetchCatalog(s.store.get())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *server) peerItem(w http.ResponseWriter, r *http.Request) {
	if !s.authorizePeer(w, r) {
		return
	}
	item, err := fetchSingleItem(s.store.get(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *server) peerImage(w http.ResponseWriter, r *http.Request) {
	if s.authorizePeer(w, r) {
		proxyJellyfinImage(w, s.store.get(), r.PathValue("id"))
	}
}

func (s *server) peerDownload(w http.ResponseWriter, r *http.Request) {
	if s.authorizePeer(w, r) {
		proxyJellyfinDownload(w, s.store.get(), r.PathValue("id"))
	}
}

func (s *server) authorizePeer(w http.ResponseWriter, r *http.Request) bool {
	if !peerAuthorized(r, s.store.get().NetworkKey) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return false
	}
	return true
}

func findPeer(peers []Peer, id string) (Peer, bool) {
	for _, peer := range peers {
		if peer.ID == id {
			return peer, true
		}
	}
	return Peer{}, false
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
