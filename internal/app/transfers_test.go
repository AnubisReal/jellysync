package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDestinationForMovieAndEpisode(t *testing.T) {
	movie := destinationFor(CatalogItem{Type: "Movie", Name: "Dune", ProductionYear: 2021}, "Dune.mkv", "/media/movies", "/media/series")
	if movie != filepath.Join("/media/movies", "Dune (2021)", "Dune.mkv") {
		t.Fatalf("unexpected movie destination: %s", movie)
	}
	episode := destinationFor(CatalogItem{Type: "Episode", SeriesName: "The Last of Us", ParentIndexNumber: 1}, "S01E02.mkv", "/media/movies", "/media/series")
	if episode != filepath.Join("/media/series", "The Last of Us", "Season 01", "S01E02.mkv") {
		t.Fatalf("unexpected episode destination: %s", episode)
	}
}

func TestMoveAcrossFilesystems(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "downloads", "movie.partial")
	destination := filepath.Join(root, "movies", "Movie", "movie.mkv")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := moveAcrossFilesystems(source, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "media" {
		t.Fatalf("unexpected destination: %q, %v", content, err)
	}
}

func TestPasswordHash(t *testing.T) {
	cfg := Config{AdminSalt: randomToken(16)}
	cfg.AdminHash = hashPassword("a-secure-password", cfg.AdminSalt)
	if !passwordMatches("a-secure-password", cfg) {
		t.Fatal("expected password to match")
	}
	if passwordMatches("incorrect-password", cfg) {
		t.Fatal("incorrect password matched")
	}
}
