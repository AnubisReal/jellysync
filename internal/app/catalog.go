package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type CatalogItem struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Type              string            `json:"type"`
	Overview          string            `json:"overview,omitempty"`
	ProductionYear    int               `json:"productionYear,omitempty"`
	DateCreated       time.Time         `json:"dateCreated,omitempty"`
	SeriesName        string            `json:"seriesName,omitempty"`
	SeriesID          string            `json:"seriesId,omitempty"`
	SeasonName        string            `json:"seasonName,omitempty"`
	ParentIndexNumber int               `json:"seasonNumber,omitempty"`
	IndexNumber       int               `json:"episodeNumber,omitempty"`
	RunTimeTicks      int64             `json:"runTimeTicks,omitempty"`
	Size              int64             `json:"size,omitempty"`
	FileName          string            `json:"fileName,omitempty"`
	ProviderIDs       map[string]string `json:"providerIds,omitempty"`
	HasImage          bool              `json:"hasImage"`
	SourceID          string            `json:"sourceId"`
	SourceName        string            `json:"sourceName"`
}

type jellyfinItem struct {
	ID                string            `json:"Id"`
	Name              string            `json:"Name"`
	Type              string            `json:"Type"`
	Overview          string            `json:"Overview"`
	ProductionYear    int               `json:"ProductionYear"`
	DateCreated       time.Time         `json:"DateCreated"`
	SeriesName        string            `json:"SeriesName"`
	SeriesID          string            `json:"SeriesId"`
	SeasonName        string            `json:"SeasonName"`
	ParentIndexNumber int               `json:"ParentIndexNumber"`
	IndexNumber       int               `json:"IndexNumber"`
	RunTimeTicks      int64             `json:"RunTimeTicks"`
	Path              string            `json:"Path"`
	ProviderIDs       map[string]string `json:"ProviderIds"`
	ImageTags         map[string]string `json:"ImageTags"`
	MediaSources      []struct {
		Size int64 `json:"Size"`
	} `json:"MediaSources"`
}

type jellyfinItemsResponse struct {
	Items            []jellyfinItem `json:"Items"`
	TotalRecordCount int            `json:"TotalRecordCount"`
}

func fetchCatalog(cfg Config) ([]CatalogItem, error) {
	query := url.Values{}
	query.Set("Recursive", "true")
	query.Set("IncludeItemTypes", "Movie,Series,Episode")
	query.Set("Fields", "Overview,ProviderIds,Path,DateCreated,MediaSources")
	query.Set("SortBy", "DateCreated")
	query.Set("SortOrder", "Descending")
	query.Set("EnableImages", "true")
	query.Set("Limit", "1000")
	requestURL := cfg.JellyfinURL + "/Items?" + query.Encode()
	request, err := jellyfinRequest(http.MethodGet, requestURL, cfg.JellyfinAPIKey)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer el catálogo de Jellyfin: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Jellyfin rechazó el catálogo (%s)", response.Status)
	}
	var result jellyfinItemsResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&result); err != nil {
		return nil, errors.New("Jellyfin devolvió un catálogo no válido")
	}
	items := make([]CatalogItem, 0, len(result.Items))
	for _, item := range result.Items {
		size := int64(0)
		if len(item.MediaSources) > 0 {
			size = item.MediaSources[0].Size
		}
		items = append(items, CatalogItem{
			ID: item.ID, Name: item.Name, Type: item.Type, Overview: item.Overview,
			ProductionYear: item.ProductionYear, DateCreated: item.DateCreated,
			SeriesName: item.SeriesName, SeriesID: item.SeriesID, SeasonName: item.SeasonName,
			ParentIndexNumber: item.ParentIndexNumber, IndexNumber: item.IndexNumber,
			RunTimeTicks: item.RunTimeTicks, Size: size, FileName: pathBase(item.Path),
			ProviderIDs: item.ProviderIDs, HasImage: item.ImageTags["Primary"] != "",
			SourceID: cfg.NodeID, SourceName: cfg.NodeName,
		})
	}
	return items, nil
}

func fetchSingleItem(cfg Config, itemID string) (CatalogItem, error) {
	query := url.Values{}
	query.Set("Ids", itemID)
	query.Set("Fields", "ProviderIds,Path,MediaSources")
	query.Set("Limit", "1")
	request, err := jellyfinRequest(http.MethodGet, cfg.JellyfinURL+"/Items?"+query.Encode(), cfg.JellyfinAPIKey)
	if err != nil {
		return CatalogItem{}, err
	}
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return CatalogItem{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return CatalogItem{}, fmt.Errorf("Jellyfin no encontró el elemento (%s)", response.Status)
	}
	var result jellyfinItemsResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&result); err != nil {
		return CatalogItem{}, err
	}
	if len(result.Items) == 0 || result.Items[0].ID != itemID {
		return CatalogItem{}, errors.New("Jellyfin no encontró el elemento")
	}
	item := result.Items[0]
	size := int64(0)
	if len(item.MediaSources) > 0 {
		size = item.MediaSources[0].Size
	}
	return CatalogItem{
		ID: item.ID, Name: item.Name, Type: item.Type, ProductionYear: item.ProductionYear,
		SeriesName: item.SeriesName, SeriesID: item.SeriesID, SeasonName: item.SeasonName,
		ParentIndexNumber: item.ParentIndexNumber, IndexNumber: item.IndexNumber,
		FileName: pathBase(item.Path), Size: size, SourceID: cfg.NodeID, SourceName: cfg.NodeName,
	}, nil
}

func jellyfinRequest(method, requestURL, apiKey string) (*http.Request, error) {
	request, err := http.NewRequest(method, requestURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Emby-Token", apiKey)
	return request, nil
}

func proxyJellyfinImage(w http.ResponseWriter, cfg Config, itemID string) {
	requestURL := cfg.JellyfinURL + "/Items/" + url.PathEscape(itemID) + "/Images/Primary?maxWidth=600&quality=85"
	request, err := jellyfinRequest(http.MethodGet, requestURL, cfg.JellyfinAPIKey)
	if err != nil {
		http.Error(w, "imagen no válida", http.StatusBadRequest)
		return
	}
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
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

func proxyJellyfinDownload(w http.ResponseWriter, cfg Config, itemID string) {
	requestURL := cfg.JellyfinURL + "/Items/" + url.PathEscape(itemID) + "/Download"
	request, err := jellyfinRequest(http.MethodGet, requestURL, cfg.JellyfinAPIKey)
	if err != nil {
		http.Error(w, "archivo no válido", http.StatusBadRequest)
		return
	}
	request.Header.Set("Accept", "application/octet-stream")
	response, err := (&http.Client{Timeout: 0}).Do(request)
	if err != nil {
		http.Error(w, "archivo no disponible", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		http.Error(w, "archivo no disponible", response.StatusCode)
		return
	}
	for _, header := range []string{"Content-Type", "Content-Length", "Content-Disposition", "Accept-Ranges", "Content-Range"} {
		if value := response.Header.Get(header); value != "" {
			w.Header().Set(header, value)
		}
	}
	if response.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(response.ContentLength, 10))
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func pathBase(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	parts := strings.Split(value, "/")
	return parts[len(parts)-1]
}
