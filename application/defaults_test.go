package application

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultApplicationRequiresNoScenario(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "new-workspace")
	app, err := DefaultApplication(Options{DataDirectory: directory})
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Workers) != 0 || len(app.Resources) != 0 {
		t.Fatal("empty workspace must not start crawl jobs")
	}
	if app.Config.API.Address != "127.0.0.1:8080" || app.Config.API.OperationsPath != filepath.Join(directory, "state.json") {
		t.Fatal(app.Config.API)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("construction must not create storage", err)
	}
}
