package app

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFetchJellyfinLibraries(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/Library/VirtualFolders" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-Emby-Token") != "secret" {
			t.Fatal("API key was not sent")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`[{"Name":"Películas","CollectionType":"movies","Locations":["/media/movies"]}]`)),
			Request:    r,
		}, nil
	})}

	libraries, err := fetchJellyfinLibrariesWithClient(client, "http://jellyfin:8096", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if len(libraries) != 1 || libraries[0].Name != "Películas" {
		t.Fatalf("unexpected libraries: %#v", libraries)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
