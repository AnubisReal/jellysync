package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchSingleItemUsesItemsQueryWithMediaSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Items" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("Ids") != "item-123" {
			t.Fatalf("unexpected item id: %s", r.URL.Query().Get("Ids"))
		}
		if r.URL.Query().Get("Fields") != "ProviderIds,Path,MediaSources" {
			t.Fatalf("unexpected fields: %s", r.URL.Query().Get("Fields"))
		}
		if r.Header.Get("X-Emby-Token") != "api-key" {
			t.Fatal("missing Jellyfin API key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[{"Id":"item-123","Name":"Movie","Type":"Movie","Path":"/media/Movie.mkv","MediaSources":[{"Size":123456}]}],"TotalRecordCount":1}`))
	}))
	defer server.Close()

	item, err := fetchSingleItem(Config{JellyfinURL: server.URL, JellyfinAPIKey: "api-key", NodeID: "source", NodeName: "Origin"}, "item-123")
	if err != nil {
		t.Fatal(err)
	}
	if item.Size != 123456 || item.FileName != "Movie.mkv" {
		t.Fatalf("unexpected item: %#v", item)
	}
}

func TestFetchSingleItemRejectsEmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[],"TotalRecordCount":0}`))
	}))
	defer server.Close()

	if _, err := fetchSingleItem(Config{JellyfinURL: server.URL}, "missing"); err == nil {
		t.Fatal("expected missing item error")
	}
}
