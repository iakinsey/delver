package api

import (
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestServeUIRoutesAndMissingAssets(t *testing.T) {
	files := fstest.MapFS{"index.html": {Data: []byte("workspace")}, "assets/app.js": {Data: []byte("script")}}
	for _, tc := range []struct {
		path string
		code int
		body string
	}{
		{"/", 200, "workspace"}, {"/crawls/example", 200, "workspace"}, {"/assets/app.js", 200, "script"}, {"/assets/missing.js", 404, "404 page not found\n"},
	} {
		rec := httptest.NewRecorder()
		ServeUI(files, rec, httptest.NewRequest("GET", tc.path, nil))
		if rec.Code != tc.code || rec.Body.String() != tc.body {
			t.Fatalf("%s: %d %q", tc.path, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	ServeUI(files, rec, httptest.NewRequest("HEAD", "/", nil))
	if rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatal("HEAD must return headers without body")
	}
}
