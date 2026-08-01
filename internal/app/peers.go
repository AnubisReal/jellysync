package app

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type peerRegistration struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

func peerAuthorized(r *http.Request, expected string) bool {
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if provided == "" || expected == "" || len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func registerWithCoordinator(cfg Config) (Peer, error) {
	if cfg.Mode != "node" {
		return Peer{}, nil
	}
	body, err := json.Marshal(peerRegistration{ID: cfg.NodeID, Name: cfg.NodeName, URL: cfg.PublicURL})
	if err != nil {
		return Peer{}, err
	}
	request, err := http.NewRequest(http.MethodPost, cfg.Coordinator+"/peer/v1/register", strings.NewReader(string(body)))
	if err != nil {
		return Peer{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+cfg.NetworkKey)
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return Peer{}, fmt.Errorf("no se pudo registrar en el coordinador: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 2<<10))
		return Peer{}, fmt.Errorf("el coordinador rechazó el registro (%s): %s", response.Status, strings.TrimSpace(string(message)))
	}
	var coordinator Peer
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<10)).Decode(&coordinator); err != nil {
		return Peer{}, errors.New("el coordinador devolvió una identidad no válida")
	}
	// El nodo ya conoce la URL utilizada para alcanzar al coordinador; es la
	// dirección más fiable incluso si el coordinador no configuró PublicURL.
	coordinator.URL = cfg.Coordinator
	return coordinator, nil
}

func fetchPeerCatalog(peer Peer, networkKey string) ([]CatalogItem, error) {
	request, err := http.NewRequest(http.MethodGet, peer.URL+"/peer/v1/catalog", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+networkKey)
	response, err := (&http.Client{Timeout: 35 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s respondió %s", peer.Name, response.Status)
	}
	var items []CatalogItem
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&items); err != nil {
		return nil, err
	}
	return items, nil
}

func requestPeer(peer Peer, networkKey, path string) (*http.Response, error) {
	request, err := http.NewRequest(http.MethodGet, peer.URL+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+networkKey)
	return (&http.Client{Timeout: 0}).Do(request)
}

func peerPath(kind, itemID string) string {
	return "/peer/v1/" + kind + "/" + url.PathEscape(itemID)
}
