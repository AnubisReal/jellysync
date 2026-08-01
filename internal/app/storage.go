package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type StoragePaths struct {
	Root        string `json:"root"`
	Label       string `json:"label"`
	MoviesDir   string `json:"moviesDir"`
	SeriesDir   string `json:"seriesDir"`
	DownloadDir string `json:"downloadDir"`
}

type DirectoryListing struct {
	Root        string   `json:"root"`
	Current     string   `json:"current"`
	Parent      string   `json:"parent,omitempty"`
	Directories []string `json:"directories"`
}

func storageRoot() string {
	root := env("JELLYSYNC_STORAGE_ROOT", "./data/storage")
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	return filepath.Clean(abs)
}

func defaultStoragePaths() StoragePaths {
	root := storageRoot()
	return StoragePaths{
		Root: root, Label: env("JELLYSYNC_STORAGE_LABEL", root),
		MoviesDir: filepath.Join(root, "Movies"), SeriesDir: filepath.Join(root, "Series"),
		DownloadDir: filepath.Join(root, "Downloads", "Jellysync"),
	}
}

func currentStoragePaths(cfg Config) StoragePaths {
	defaults := defaultStoragePaths()
	if cfg.MoviesDir != "" {
		defaults.MoviesDir, defaults.SeriesDir, defaults.DownloadDir = cfg.MoviesDir, cfg.SeriesDir, cfg.DownloadDir
	}
	return defaults
}

func normalizeStoragePath(value string) (string, error) {
	root := storageRoot()
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("la ruta no puede estar vacía")
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(root, value)
	}
	value = filepath.Clean(value)
	relative, err := filepath.Rel(root, value)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("la ruta %s está fuera del volumen autorizado", value)
	}
	return value, nil
}

func validateStoragePaths(input StoragePaths) (StoragePaths, error) {
	movies, err := normalizeStoragePath(input.MoviesDir)
	if err != nil {
		return StoragePaths{}, err
	}
	series, err := normalizeStoragePath(input.SeriesDir)
	if err != nil {
		return StoragePaths{}, err
	}
	downloads, err := normalizeStoragePath(input.DownloadDir)
	if err != nil {
		return StoragePaths{}, err
	}
	if movies == series || movies == downloads || series == downloads {
		return StoragePaths{}, errors.New("películas, series y descargas deben utilizar carpetas diferentes")
	}
	for _, directory := range []string{movies, series, downloads} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return StoragePaths{}, fmt.Errorf("no se pudo crear %s: %w", directory, err)
		}
		if err := ensureResolvedWithinRoot(directory); err != nil {
			return StoragePaths{}, err
		}
		probe, err := os.CreateTemp(directory, ".jellysync-write-test-")
		if err != nil {
			return StoragePaths{}, fmt.Errorf("JellySync no puede escribir en %s", directory)
		}
		name := probe.Name()
		_ = probe.Close()
		_ = os.Remove(name)
	}
	return StoragePaths{Root: storageRoot(), Label: env("JELLYSYNC_STORAGE_LABEL", storageRoot()), MoviesDir: movies, SeriesDir: series, DownloadDir: downloads}, nil
}

func browseDirectories(value string) (DirectoryListing, error) {
	current, err := normalizeStoragePath(value)
	if err != nil {
		return DirectoryListing{}, err
	}
	info, err := os.Stat(current)
	if err != nil || !info.IsDir() {
		return DirectoryListing{}, errors.New("la carpeta seleccionada no existe")
	}
	if err := ensureResolvedWithinRoot(current); err != nil {
		return DirectoryListing{}, err
	}
	entries, err := os.ReadDir(current)
	if err != nil {
		return DirectoryListing{}, errors.New("no se pudo leer la carpeta")
	}
	listing := DirectoryListing{Root: storageRoot(), Current: current, Directories: []string{}}
	if current != listing.Root {
		listing.Parent = filepath.Dir(current)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		listing.Directories = append(listing.Directories, filepath.Join(current, entry.Name()))
	}
	sort.Strings(listing.Directories)
	return listing, nil
}

func ensureResolvedWithinRoot(target string) error {
	resolvedRoot, err := filepath.EvalSymlinks(storageRoot())
	if err != nil {
		return errors.New("no se pudo resolver el volumen de almacenamiento")
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return errors.New("no se pudo resolver la carpeta seleccionada")
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("la carpeta seleccionada sale del volumen autorizado mediante un enlace simbólico")
	}
	return nil
}

func (s *configStore) updateStorage(paths StoragePaths) error {
	cfg := s.get()
	cfg.MoviesDir, cfg.SeriesDir, cfg.DownloadDir = paths.MoviesDir, paths.SeriesDir, paths.DownloadDir
	return s.write(cfg)
}
