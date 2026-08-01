package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateStoragePathsCreatesWritableDirectories(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JELLYSYNC_STORAGE_ROOT", root)
	paths, err := validateStoragePaths(StoragePaths{
		MoviesDir: "Movies", SeriesDir: "Series", DownloadDir: "Downloads/Jellysync",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{paths.MoviesDir, paths.SeriesDir, paths.DownloadDir} {
		if info, err := os.Stat(directory); err != nil || !info.IsDir() {
			t.Fatalf("directory was not created: %s (%v)", directory, err)
		}
	}
}

func TestStoragePathCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JELLYSYNC_STORAGE_ROOT", root)
	outside := filepath.Join(filepath.Dir(root), "outside")
	if _, err := normalizeStoragePath(outside); err == nil {
		t.Fatal("expected path outside root to be rejected")
	}
}

func TestBrowseDirectoriesOnlyListsFolders(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JELLYSYNC_STORAGE_ROOT", root)
	if err := os.Mkdir(filepath.Join(root, "Movies"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	listing, err := browseDirectories(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Directories) != 1 || listing.Directories[0] != filepath.Join(root, "Movies") {
		t.Fatalf("unexpected listing: %#v", listing.Directories)
	}
}
