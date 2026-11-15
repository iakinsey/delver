package application

import (
	"fmt"
	"path/filepath"

	"github.com/iakinsey/delver/config"
)

// Options configures application-level storage and listeners without a pipeline file.
type Options struct {
	DataDirectory string
	ImportPath    string
	Address       string
}

// DefaultApplication constructs a valid empty workspace. It performs no I/O and
// creates no publishers, queues, or crawl jobs.
func DefaultApplication(options Options) (config.Application, error) {
	directory := options.DataDirectory
	if directory == "" {
		directory = filepath.Join("run-data", "workspace")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return config.Application{}, fmt.Errorf("workspace path: %w", err)
	}
	c := config.LoadConfig()
	c.API.Address = options.Address
	if c.API.Address == "" {
		c.API.Address = "127.0.0.1:8080"
	}
	c.API.DashDBPath = filepath.Join(absolute, "dash.db")
	c.API.OperationsPath = filepath.Join(absolute, "state.json")
	c.Metrics.Enabled = false
	c.Streamer.Address = "127.0.0.1:26548"
	c.Workers.WorkerCounts = 4
	a := config.Application{Config: c, Resources: []config.Resource{}, Workers: []config.Worker{}}
	return a, Validate(a)
}
