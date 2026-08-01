package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Peer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type Config struct {
	Configured     bool              `json:"configured"`
	Mode           string            `json:"mode"`
	NodeID         string            `json:"nodeId"`
	NodeName       string            `json:"nodeName"`
	PublicURL      string            `json:"publicUrl,omitempty"`
	Coordinator    string            `json:"coordinator,omitempty"`
	NetworkKey     string            `json:"networkKey,omitempty"`
	JellyfinURL    string            `json:"jellyfinUrl,omitempty"`
	JellyfinAPIKey string            `json:"jellyfinApiKey,omitempty"`
	Libraries      []JellyfinLibrary `json:"libraries,omitempty"`
	Peers          []Peer            `json:"peers,omitempty"`
	AdminSalt      string            `json:"adminSalt,omitempty"`
	AdminHash      string            `json:"adminHash,omitempty"`
	AdminPassword  string            `json:"adminPassword,omitempty"`
	MoviesDir      string            `json:"moviesDir,omitempty"`
	SeriesDir      string            `json:"seriesDir,omitempty"`
	DownloadDir    string            `json:"downloadDir,omitempty"`
}

type PublicConfig struct {
	Configured        bool              `json:"configured"`
	Mode              string            `json:"mode,omitempty"`
	NodeID            string            `json:"nodeId,omitempty"`
	NodeName          string            `json:"nodeName,omitempty"`
	PublicURL         string            `json:"publicUrl,omitempty"`
	Coordinator       string            `json:"coordinator,omitempty"`
	InviteCode        string            `json:"inviteCode,omitempty"`
	JellyfinURL       string            `json:"jellyfinUrl,omitempty"`
	JellyfinConnected bool              `json:"jellyfinConnected"`
	Libraries         []JellyfinLibrary `json:"libraries,omitempty"`
	Peers             []Peer            `json:"peers,omitempty"`
	Authenticated     bool              `json:"authenticated"`
	NeedsClaim        bool              `json:"needsClaim"`
	StorageRoot       string            `json:"storageRoot,omitempty"`
	StorageLabel      string            `json:"storageLabel,omitempty"`
	MoviesDir         string            `json:"moviesDir,omitempty"`
	SeriesDir         string            `json:"seriesDir,omitempty"`
	DownloadDir       string            `json:"downloadDir,omitempty"`
}

type configStore struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

func newConfigStore(dataDir string) (*configStore, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	s := &configStore{path: filepath.Join(dataDir, "config.json")}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.cfg); err != nil {
		return nil, err
	}
	changed := false
	if s.cfg.Configured && s.cfg.NodeID == "" {
		s.cfg.NodeID = randomToken(12)
		changed = true
	}
	if s.cfg.Configured && s.cfg.Mode == "coordinator" && s.cfg.NetworkKey == "" {
		s.cfg.NetworkKey = randomToken(24)
		changed = true
	}
	defaults := defaultStoragePaths()
	if s.cfg.Configured && s.cfg.MoviesDir == "" {
		s.cfg.MoviesDir, s.cfg.SeriesDir, s.cfg.DownloadDir = defaults.MoviesDir, defaults.SeriesDir, defaults.DownloadDir
		changed = true
	}
	if changed {
		if err := s.write(s.cfg); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *configStore) get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *configStore) public(includeInvite bool) PublicConfig {
	cfg := s.get()
	libraries := make([]JellyfinLibrary, 0, len(cfg.Libraries))
	for _, library := range cfg.Libraries {
		libraries = append(libraries, JellyfinLibrary{Name: library.Name, CollectionType: library.CollectionType})
	}
	public := PublicConfig{
		Configured: cfg.Configured, Mode: cfg.Mode, NodeID: cfg.NodeID, NodeName: cfg.NodeName,
		PublicURL: cfg.PublicURL, Coordinator: cfg.Coordinator,
		JellyfinURL: cfg.JellyfinURL, JellyfinConnected: cfg.JellyfinAPIKey != "",
		Libraries: libraries, Peers: cfg.Peers,
		StorageRoot: storageRoot(), StorageLabel: env("JELLYSYNC_STORAGE_LABEL", storageRoot()),
		MoviesDir: cfg.MoviesDir, SeriesDir: cfg.SeriesDir, DownloadDir: cfg.DownloadDir,
	}
	if includeInvite && cfg.Mode == "coordinator" {
		public.InviteCode = cfg.NetworkKey
	}
	return public
}

func (s *configStore) save(cfg Config) error {
	cfg.NodeName = strings.TrimSpace(cfg.NodeName)
	cfg.PublicURL = strings.TrimRight(strings.TrimSpace(cfg.PublicURL), "/")
	cfg.Coordinator = strings.TrimRight(strings.TrimSpace(cfg.Coordinator), "/")
	cfg.JellyfinURL = strings.TrimRight(strings.TrimSpace(cfg.JellyfinURL), "/")
	cfg.JellyfinAPIKey = strings.TrimSpace(cfg.JellyfinAPIKey)
	cfg.NetworkKey = strings.TrimSpace(cfg.NetworkKey)
	if cfg.MoviesDir == "" || cfg.SeriesDir == "" || cfg.DownloadDir == "" {
		defaults := defaultStoragePaths()
		cfg.MoviesDir, cfg.SeriesDir, cfg.DownloadDir = defaults.MoviesDir, defaults.SeriesDir, defaults.DownloadDir
	}
	if cfg.NodeName == "" {
		return errors.New("el nombre del nodo es obligatorio")
	}
	if cfg.Mode != "coordinator" && cfg.Mode != "node" {
		return errors.New("el modo seleccionado no es válido")
	}
	if cfg.NodeID == "" {
		cfg.NodeID = randomToken(12)
	}
	if cfg.Mode == "coordinator" && cfg.NetworkKey == "" {
		cfg.NetworkKey = randomToken(24)
	}
	if cfg.Mode == "node" {
		if cfg.Coordinator == "" || cfg.NetworkKey == "" {
			return errors.New("la dirección y el código de invitación del coordinador son obligatorios")
		}
		if cfg.PublicURL == "" {
			return errors.New("la dirección pública es obligatoria para que el coordinador pueda consultar este nodo")
		}
	}
	if cfg.JellyfinURL == "" || cfg.JellyfinAPIKey == "" {
		return errors.New("la dirección y la API key de Jellyfin son obligatorias")
	}
	if cfg.AdminHash == "" {
		if len(cfg.AdminPassword) < 10 {
			return errors.New("la contraseña administrativa debe tener al menos 10 caracteres")
		}
		cfg.AdminSalt = randomToken(16)
		cfg.AdminHash = hashPassword(cfg.AdminPassword, cfg.AdminSalt)
	}
	cfg.AdminPassword = ""
	cfg.Configured = true
	return s.write(cfg)
}

func (s *configStore) write(cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	return nil
}

func (s *configStore) updateJellyfin(url, apiKey string, libraries []JellyfinLibrary) error {
	cfg := s.get()
	cfg.JellyfinURL = strings.TrimRight(strings.TrimSpace(url), "/")
	cfg.JellyfinAPIKey = strings.TrimSpace(apiKey)
	cfg.Libraries = libraries
	return s.save(cfg)
}

func (s *configStore) addPeer(peer Peer) error {
	peer.URL = strings.TrimRight(strings.TrimSpace(peer.URL), "/")
	if peer.ID == "" || peer.Name == "" || peer.URL == "" {
		return errors.New("los datos del nodo no están completos")
	}
	cfg := s.get()
	for i := range cfg.Peers {
		if cfg.Peers[i].ID == peer.ID {
			cfg.Peers[i] = peer
			return s.write(cfg)
		}
	}
	cfg.Peers = append(cfg.Peers, peer)
	return s.write(cfg)
}

func (s *configStore) setAdminPassword(password string) error {
	cfg := s.get()
	if cfg.AdminHash != "" {
		return errors.New("la cuenta administrativa ya está configurada")
	}
	cfg.AdminPassword = password
	return s.save(cfg)
}

func randomToken(bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
