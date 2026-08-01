package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type DownloadJob struct {
	ID          string    `json:"id"`
	ItemID      string    `json:"itemId"`
	Name        string    `json:"name"`
	SourceID    string    `json:"sourceId"`
	SourceName  string    `json:"sourceName"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	BytesDone   int64     `json:"bytesDone"`
	BytesTotal  int64     `json:"bytesTotal"`
	Checksum    string    `json:"checksum,omitempty"`
	Destination string    `json:"destination,omitempty"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type downloadRequest struct {
	SourceID string `json:"sourceId"`
	ItemID   string `json:"itemId"`
}

type transferManager struct {
	mu     sync.RWMutex
	jobs   map[string]*DownloadJob
	store  *configStore
	logger logger
}

type logger interface {
	Info(string, ...any)
	Error(string, ...any)
}

func newTransferManager(store *configStore, log logger) (*transferManager, error) {
	m := &transferManager{
		jobs: make(map[string]*DownloadJob), store: store, logger: log,
	}
	if err := os.MkdirAll(storageRoot(), 0o755); err != nil {
		return nil, fmt.Errorf("no se pudo preparar el almacenamiento: %w", err)
	}
	return m, nil
}

func (m *transferManager) list() []DownloadJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	jobs := make([]DownloadJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, *job)
	}
	return jobs
}

func (m *transferManager) start(peer Peer, itemID string) DownloadJob {
	now := time.Now().UTC()
	job := &DownloadJob{ID: randomToken(10), ItemID: itemID, SourceID: peer.ID, SourceName: peer.Name, Status: "queued", CreatedAt: now, UpdatedAt: now}
	m.mu.Lock()
	m.jobs[job.ID] = job
	m.mu.Unlock()
	go m.run(job.ID, peer)
	return *job
}

func (m *transferManager) update(id string, fn func(*DownloadJob)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job := m.jobs[id]; job != nil {
		fn(job)
		job.UpdatedAt = time.Now().UTC()
	}
}

func (m *transferManager) fail(id string, err error) {
	m.update(id, func(job *DownloadJob) {
		job.Status = "failed"
		job.Error = err.Error()
	})
	m.logger.Error("download failed", "job", id, "error", err)
}

func (m *transferManager) run(id string, peer Peer) {
	cfg := m.store.get()
	paths := currentStoragePaths(cfg)
	if err := os.MkdirAll(paths.DownloadDir, 0o755); err != nil {
		m.fail(id, fmt.Errorf("no se pudo preparar staging: %w", err))
		return
	}
	item, err := fetchPeerItem(peer, cfg.NetworkKey, m.get(id).ItemID)
	if err != nil {
		m.fail(id, err)
		return
	}
	if item.Type != "Movie" && item.Type != "Episode" {
		m.fail(id, errors.New("solo se pueden descargar películas o episodios"))
		return
	}
	m.update(id, func(job *DownloadJob) {
		job.Name, job.Type, job.BytesTotal, job.Status = item.Name, item.Type, item.Size, "downloading"
	})
	response, err := requestPeer(peer, cfg.NetworkKey, peerPath("download", item.ID))
	if err != nil {
		m.fail(id, err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		m.fail(id, fmt.Errorf("el nodo de origen respondió %s", response.Status))
		return
	}
	if response.ContentLength > 0 {
		m.update(id, func(job *DownloadJob) { job.BytesTotal = response.ContentLength })
	}
	filename := safeFileName(item.FileName)
	if filename == "" {
		filename = safeName(item.Name) + ".media"
	}
	partial := filepath.Join(paths.DownloadDir, id+"-"+filename+".partial")
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		m.fail(id, err)
		return
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), &progressReader{reader: response.Body, update: func(total int64) {
		m.update(id, func(job *DownloadJob) { job.BytesDone = total })
	}})
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(partial)
		m.fail(id, errors.Join(copyErr, closeErr))
		return
	}
	if expected := m.get(id).BytesTotal; expected > 0 && written != expected {
		_ = os.Remove(partial)
		m.fail(id, fmt.Errorf("tamaño incompleto: recibidos %d de %d bytes", written, expected))
		return
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	m.update(id, func(job *DownloadJob) { job.Status, job.Checksum = "moving", checksum })
	destination := destinationFor(item, filename, paths.MoviesDir, paths.SeriesDir)
	if err := moveAcrossFilesystems(partial, destination); err != nil {
		m.fail(id, err)
		return
	}
	m.update(id, func(job *DownloadJob) {
		job.Status, job.Destination, job.BytesDone = "completed", destination, written
	})
	request, _ := jellyfinRequest(http.MethodPost, cfg.JellyfinURL+"/Library/Refresh", cfg.JellyfinAPIKey)
	if request != nil {
		response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
		if err == nil {
			response.Body.Close()
		}
	}
	m.logger.Info("download completed", "job", id, "destination", destination)
}

func (m *transferManager) get(id string) DownloadJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if job := m.jobs[id]; job != nil {
		return *job
	}
	return DownloadJob{}
}

func destinationFor(item CatalogItem, filename, moviesDir, seriesDir string) string {
	if item.Type == "Movie" {
		folder := item.Name
		if item.ProductionYear > 0 {
			folder = fmt.Sprintf("%s (%d)", item.Name, item.ProductionYear)
		}
		return filepath.Join(moviesDir, safeName(folder), filename)
	}
	series := safeName(item.SeriesName)
	if series == "" {
		series = "Serie sin identificar"
	}
	season := fmt.Sprintf("Season %02d", item.ParentIndexNumber)
	return filepath.Join(seriesDir, series, season, filename)
}

type progressReader struct {
	reader io.Reader
	total  int64
	update func(int64)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.total += int64(n)
	if n > 0 {
		r.update(r.total)
	}
	return n, err
}

var unsafeName = regexp.MustCompile(`[^\p{L}\p{N} ._()\[\]-]+`)

func safeName(value string) string {
	value = unsafeName.ReplaceAllString(strings.TrimSpace(value), "_")
	value = strings.Trim(value, ". ")
	runes := []rune(value)
	if len(runes) > 180 {
		value = string(runes[:180])
	}
	return value
}

func safeFileName(value string) string {
	return safeName(filepath.Base(strings.ReplaceAll(value, "\\", "/")))
}

func moveAcrossFilesystems(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(destination); err == nil {
		return errors.New("el archivo ya existe en la biblioteca de destino")
	}
	if err := os.Rename(source, destination); err == nil {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary := destination + ".jellysync-partial"
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(temporary)
		return errors.Join(copyErr, closeErr)
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return os.Remove(source)
}

func fetchPeerItem(peer Peer, networkKey, itemID string) (CatalogItem, error) {
	response, err := requestPeer(peer, networkKey, peerPath("item", itemID))
	if err != nil {
		return CatalogItem{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return CatalogItem{}, fmt.Errorf("el origen no encontró el contenido (%s)", response.Status)
	}
	var item CatalogItem
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&item); err != nil {
		return CatalogItem{}, err
	}
	return item, nil
}
