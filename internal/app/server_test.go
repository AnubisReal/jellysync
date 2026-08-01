package app

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPAHandlerServesIndexForApplicationRoutes(t *testing.T) {
	web := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("JellySync application")},
		"app.js":     &fstest.MapFile{Data: []byte("console.log('ok')")},
	}
	handler := spaHandler(web)

	for _, route := range []string{"/inicio", "/descargas", "/servidores", "/ajustes", "/contenido/node/item"} {
		request := httptest.NewRequest(http.MethodGet, route, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "JellySync application") {
			t.Fatalf("route %s did not serve the application: %d %q", route, response.Code, response.Body.String())
		}
	}
}

func TestSPAHandlerServesAssetsAndRejectsMissingAssets(t *testing.T) {
	web := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("index")},
		"app.js":     &fstest.MapFile{Data: []byte("asset")},
	}
	handler := spaHandler(fs.FS(web))

	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != "asset" {
		t.Fatalf("asset was not served: %d %q", asset.Code, asset.Body.String())
	}

	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/missing.css", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing asset returned %d", missing.Code)
	}
}
