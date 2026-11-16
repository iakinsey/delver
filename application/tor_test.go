package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/flow"
	"github.com/stretchr/testify/require"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type torObservation struct {
	Runtime struct {
		State   string           `json:"state"`
		Runtime *RuntimeSnapshot `json:"runtime"`
	} `json:"runtime"`
	Runs []CrawlRun `json:"runs"`
}

func readTorObservation(t *testing.T, w *Workspace, id string) torObservation {
	t.Helper()
	value, err := w.snapshot(id)
	require.NoError(t, err)
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	var v torObservation
	require.NoError(t, json.Unmarshal(raw, &v))
	return v
}
func waitTorState(t *testing.T, w *Workspace, id, state string) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if readTorObservation(t, w, id).Runtime.State == state {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("crawl did not reach %s", state)
}
func torCommand(t *testing.T, w *Workspace, id, action string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"crawl_id": id, "id": "tor-" + action + "-" + fmt.Sprint(time.Now().UnixNano()), "action": action})
	_, err := w.HandleCrawls(context.Background(), "command", raw)
	require.NoError(t, err)
}

// Count only sockets owned by this isolated test process, never other crawls.
func torProxyConnections(t *testing.T, proxyURL string) int {
	t.Helper()
	proxy, err := url.Parse(proxyURL)
	require.NoError(t, err)
	port, err := strconv.Atoi(proxy.Port())
	require.NoError(t, err)
	fds, err := filepath.Glob("/proc/self/fd/*")
	require.NoError(t, err)
	owned := map[string]bool{}
	for _, fd := range fds {
		target, e := os.Readlink(fd)
		if e == nil && strings.HasPrefix(target, "socket:[") {
			owned[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] = true
		}
	}
	count := 0
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, e := os.ReadFile(path)
		require.NoError(t, e, "live Tor shutdown verification requires Linux /proc socket tables")
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) > 9 && f[3] == "01" && strings.HasSuffix(f[2], fmt.Sprintf(":%04X", port)) && owned[f[9]] {
				count++
			}
		}
	}
	return count
}
func TestLiveTor(t *testing.T) {
	if os.Getenv("DELVER_TOR_LIVE") != "1" {
		t.Skip("opt-in: make test-tor TOR_CONFIG=/private/crawl-definition.json")
	}
	raw, err := os.ReadFile(os.Getenv("DELVER_TOR_CONFIG"))
	require.NoError(t, err, "private test seed definition required")
	d, err := decodeDefinition(raw)
	require.NoError(t, err)
	require.NotEmpty(t, d.Seeds)
	for _, seed := range append(append([]string{}, d.Seeds...), d.Feeds...) {
		u, e := url.Parse(seed)
		require.NoError(t, e)
		require.True(t, strings.HasSuffix(u.Hostname(), ".onion"), "Tor validation permits onion seeds only")
	}
	d.Name = "Tor validation"
	d.Network.RequireProxy = true
	d.Network.RestrictHosts = true
	d.Network.AllowedHostSuffixes = []string{"onion"}
	d.Network.RespectRobots = boolPointer(false)
	proxy := os.Getenv("DELVER_TOR_PROXY")
	require.NotEmpty(t, proxy)
	d.Proxy = ProxySettings{Mode: "socks5", URL: proxy}
	if d.Fetch == nil {
		p := flow.Default()
		d.Fetch = &p
	}
	d.Fetch.Mode = "adaptive"
	d.Termination = Termination{}
	d.Revisit = "24h"
	root := os.Getenv("DELVER_VALIDATION_ROOT")
	require.NotEmpty(t, root)
	require.NoError(t, os.MkdirAll(root, 0700))
	storage, err := os.MkdirTemp(root, "tor-workspace-")
	require.NoError(t, err)
	configJSON, err := json.MarshalIndent(d, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "tor-definition.json"), configJSON, 0600))
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: storage})
	require.NoError(t, err)
	var id string
	defer func() {
		if w != nil {
			if id != "" {
				torCommand(t, w, id, "stop")
			}
			w.Close()
		}
	}()
	record := createWorkspaceCrawl(t, w, d)
	id = record["id"].(string)
	startWorkspaceCrawl(t, w, record)
	initial := readTorObservation(t, w, id)
	require.Len(t, initial.Runs, 1)
	runID := initial.Runs[0].ID
	samples, err := os.OpenFile(filepath.Join(root, "tor-observations.jsonl"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	require.NoError(t, err)
	defer samples.Close()
	started := time.Now()
	nextReport := 5 * time.Minute
	paused, restarted := false, false
	for time.Since(started) < 30*time.Minute {
		elapsed := time.Since(started)
		observation := readTorObservation(t, w, id)
		require.Equal(t, runID, observation.Runs[len(observation.Runs)-1].ID)
		require.Equal(t, "running", observation.Runtime.State)
		frame := observation.Runtime.Runtime
		require.NotNil(t, frame)
		require.Empty(t, frame.Health)
		for name, q := range frame.QueueStates {
			if name != "frontier" {
				require.Zero(t, q["failed"], name)
			}
		}
		require.NotNil(t, frame.Fetch)
		require.LessOrEqual(t, frame.Fetch.Active, frame.Fetch.ProcessLimit)
		require.GreaterOrEqual(t, frame.Fetch.Effective, d.Fetch.Minimum)
		require.LessOrEqual(t, frame.Fetch.Effective, d.Fetch.Maximum)
		require.Equal(t, d.Network.UserAgent, observation.Runs[len(observation.Runs)-1].Definition.Network.UserAgent)
		row := map[string]interface{}{"elapsed_seconds": elapsed.Seconds(), "state": observation.Runtime.State, "fetch": frame.Fetch, "frontier": frame.Frontier}
		encoded, _ := json.Marshal(row)
		_, err = samples.Write(append(encoded, '\n'))
		require.NoError(t, err)
		if elapsed >= nextReport {
			t.Logf("TOR CHECK %dm: attempts=%d successes=%d successes/min=%.2f effective=%d active=%d reason=%s adjustment=%s", int(nextReport.Minutes()), frame.Fetch.Totals.Attempts, frame.Fetch.Totals.Successes, float64(frame.Fetch.Totals.Successes)/elapsed.Minutes(), frame.Fetch.Effective, frame.Fetch.Active, frame.Fetch.Reason, frame.Fetch.LastAdjustment)
			nextReport += 5 * time.Minute
		}
		if elapsed >= 10*time.Minute && !paused {
			torCommand(t, w, id, "pause")
			waitTorState(t, w, id, "paused")
			before := readTorObservation(t, w, id).Runs[0].Elapsed
			time.Sleep(2 * time.Second)
			require.Equal(t, before, readTorObservation(t, w, id).Runs[0].Elapsed)
			torCommand(t, w, id, "resume")
			waitTorState(t, w, id, "running")
			paused = true
		}
		if elapsed >= 20*time.Minute && !restarted {
			require.NoError(t, w.Close())
			w = nil
			checkpoint, err := os.ReadFile(filepath.Join(storage, "crawls", id, "runs", runID, "state.json.fetch.json"))
			require.NoError(t, err)
			var saved struct {
				Totals flow.Totals `json:"totals"`
			}
			require.NoError(t, json.Unmarshal(checkpoint, &saved))
			w, err = NewWorkspace(context.Background(), Options{DataDirectory: storage})
			require.NoError(t, err)
			waitTorState(t, w, id, "running")
			after := readTorObservation(t, w, id)
			require.Equal(t, runID, after.Runs[0].ID)
			require.GreaterOrEqual(t, after.Runtime.Runtime.Fetch.Totals.Attempts, saved.Totals.Attempts)
			restarted = true
		}
		time.Sleep(2 * time.Second)
	}
	torCommand(t, w, id, "stop")
	waitTorState(t, w, id, "stopped")
	final := readTorObservation(t, w, id)
	require.NotNil(t, final.Runs[0].Summary)
	summary := final.Runs[0].Summary
	require.NotNil(t, summary.Fetch)
	require.Zero(t, summary.Fetch.Active)
	require.Zero(t, summary.Fetch.NetworkActive)
	require.Zero(t, summary.Fetch.ProcessActive)
	for name, worker := range summary.Workers {
		require.Zero(t, worker.Active, name)
	}
	first := summary.Fetch.Totals
	for i := 0; i < 20; i++ {
		require.Zero(t, torProxyConnections(t, proxy), "crawl retained a proxy connection after stopping")
		time.Sleep(250 * time.Millisecond)
	}
	require.Equal(t, first, readTorObservation(t, w, id).Runs[0].Summary.Fetch.Totals)
	require.NoError(t, w.Close())
	w = nil
	checkpoint, err := os.ReadFile(filepath.Join(storage, "crawls", id, "runs", runID, "state.json.fetch.json"))
	require.NoError(t, err)
	var saved struct {
		Totals flow.Totals `json:"totals"`
	}
	require.NoError(t, json.Unmarshal(checkpoint, &saved))
	require.Equal(t, first, saved.Totals)
	report := map[string]interface{}{"duration_seconds": time.Since(started).Seconds(), "crawl_id": id, "run_id": runID, "totals": first, "successes_per_minute": float64(first.Successes) / 30, "paused_and_resumed": paused, "application_restarted": restarted, "shutdown_verified": true, "proxy_connections_after_stop": 0, "proxy_modified": false}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(root, "tor-summary.json"), encoded, 0600))
	t.Logf("TOR FINAL attempts=%d successes=%d average_successes_per_minute=%.2f", first.Attempts, first.Successes, float64(first.Successes)/30)
	require.Greater(t, first.Successes, int64(0), "external Tor/destination failure: live validation inconclusive; proxy must remain unchanged")
}
