package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type JellyfinLibrary struct {
	Name           string   `json:"name"`
	CollectionType string   `json:"collectionType,omitempty"`
	Locations      []string `json:"locations,omitempty"`
}

type jellyfinCredentials struct {
	URL    string `json:"url"`
	APIKey string `json:"apiKey"`
}

func fetchJellyfinLibraries(rawURL, apiKey string) ([]JellyfinLibrary, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	return fetchJellyfinLibrariesWithClient(client, rawURL, apiKey)
}

func fetchJellyfinLibrariesWithClient(client *http.Client, rawURL, apiKey string) ([]JellyfinLibrary, error) {
	rawURL = strings.TrimRight(strings.TrimSpace(rawURL), "/")
	apiKey = strings.TrimSpace(apiKey)
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("la dirección de Jellyfin debe ser una URL HTTP o HTTPS válida")
	}
	if apiKey == "" {
		return nil, errors.New("la API key de Jellyfin es obligatoria")
	}
	request, err := http.NewRequest(http.MethodGet, rawURL+"/Library/VirtualFolders", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Emby-Token", apiKey)
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("no se pudo conectar con Jellyfin: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Jellyfin rechazó la conexión (%s)", response.Status)
	}
	var libraries []JellyfinLibrary
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if err := decoder.Decode(&libraries); err != nil {
		return nil, errors.New("Jellyfin devolvió una respuesta no válida")
	}
	return libraries, nil
}
