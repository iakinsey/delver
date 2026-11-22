package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/worker"
	"github.com/stretchr/testify/require"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDraftSaveFailureDoesNotChangeConfiguration(t *testing.T) {
	w, r := controlWorkspace(t)
	record := w.records[r["id"].(string)]
	d := record.Definition
	d.Name = "changed"
	require.NoError(t, os.Mkdir(filepath.Join(w.root, "crawls.json.tmp"), 0700))
	raw, _ := json.Marshal(map[string]interface{}{"crawl_id": record.ID, "revision": record.Revision, "definition": d})
	_, err := w.HandleCrawls(context.Background(), "draft", raw)
	require.Error(t, err)
	require.Nil(t, record.Draft)
	require.Equal(t, r["definition"].(map[string]interface{})["name"], record.Definition.Name)
	require.NoError(t, os.Remove(filepath.Join(w.root, "crawls.json.tmp")))
}
func TestFailedActivationRestoresRunningConfiguration(t *testing.T) {
	w, r := controlWorkspace(t)
	startWorkspaceCrawl(t, w, r)
	record := w.records[r["id"].(string)]
	original := record.Definition
	raw := json.RawMessage(`{"crawl_id":"` + record.ID + `","revision":` + fmt.Sprint(record.Revision) + `,"configuration_json":"{invalid"}`)
	_, err := w.HandleCrawls(context.Background(), "draft", raw)
	require.Error(t, err)
	require.Equal(t, original, record.Definition)
	detail := workspaceCall(t, w, "detail", map[string]string{"crawl_id": record.ID}).(map[string]interface{})
	require.Equal(t, "running", runtimeState(detail))
}
func TestStatusRemainsAvailableDuringStartupAndShutdown(t *testing.T) {
	s := testSupervisor(t)
	entered, release := make(chan struct{}), make(chan struct{})
	RegisterResource("slow_fixture", func(json.RawMessage) (interface{}, error) { close(entered); <-release; return struct{}{}, nil })
	defer RegisterResource("slow_fixture", nil)
	s.state.Active.Resources = []config.Resource{{Name: "slow", Type: "slow_fixture", Parameters: json.RawMessage(`{}`)}}
	started := make(chan error, 1)
	go func() { started <- s.Execute(context.Background(), "start", "", "") }()
	var err error
	require.NoError(t, err)
	<-entered
	done := make(chan error, 1)
	go func() { _, err := s.Handle(context.Background(), "status", json.RawMessage(`{}`)); done <- err }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("status blocked during startup")
	}
	_, err = s.Handle(context.Background(), "command", json.RawMessage(`{"id":"conflict","action":"stop"}`))
	require.Error(t, err)
	close(release)
	require.NoError(t, <-started)
}
func TestConcurrentCommandIDsAreIdempotent(t *testing.T) {
	w, r := controlWorkspace(t)
	startWorkspaceCrawl(t, w, r)
	var wg sync.WaitGroup
	errors := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, _ := json.Marshal(map[string]interface{}{"crawl_id": r["id"], "id": "same", "action": "pause"})
			_, err := w.HandleCrawls(context.Background(), "command", raw)
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	waitWorkspace(t, w, r["id"].(string), func(v map[string]interface{}) bool { return runtimeState(v) == "paused" })
	w.mu.Lock()
	defer w.mu.Unlock()
	count := 0
	for _, op := range w.records[r["id"].(string)].Operations {
		if op.ID == "same" {
			count++
			require.Equal(t, "completed", op.State)
		}
	}
	require.Equal(t, 1, count)
}

type shutdownFixture struct{ release chan struct{} }

func (*shutdownFixture) OnMessage(types.Message) (interface{}, error) { return nil, nil }
func (s *shutdownFixture) OnComplete()                                { <-s.release }
func TestShutdownTimeoutRetainsOwnershipUntilCleanup(t *testing.T) {
	app := config.Application{Config: config.LoadConfig()}
	app.Config.API.Enabled = false
	app.Config.Streamer.Enabled = false
	app.Config.Metrics.Enabled = false
	app.Config.ShutdownTimeout = 40 * time.Millisecond
	app.Config.API.OperationsPath = filepath.Join(t.TempDir(), "operations.json")
	release := make(chan struct{})
	RegisterWorker("shutdown_fixture", func(json.RawMessage, map[string]interface{}) (worker.Worker, error) {
		return &shutdownFixture{release}, nil
	})
	defer RegisterWorker("shutdown_fixture", nil)
	app.Resources = []config.Resource{{Name: "in", Type: "channel_queue"}, {Name: "out", Type: "channel_queue"}}
	app.Workers = []config.Worker{{Name: "blocked", Type: "shutdown_fixture", Inbox: "in", Outbox: "out", Count: 1}}
	s, err := NewSupervisor(context.Background(), app)
	require.NoError(t, err)
	defer func() { s.Close() }()
	require.Equal(t, "completed", runCommand(t, s, "start", "start").State)
	op := runCommand(t, s, "stop", "stop")
	require.Equal(t, "failed", op.State)
	require.Error(t, s.Close())
	_, err = NewSupervisor(context.Background(), app)
	require.Error(t, err)
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.Close()
		s.mu.Lock()
		released := s.lockFile == nil
		s.mu.Unlock()
		if released {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("ownership was not released after cleanup")
}

func TestCloseDuringCommandWaitsForConstructionCleanup(t *testing.T) {
	s := testSupervisor(t)
	s.state.Active.Config.ShutdownTimeout = 100 * time.Millisecond
	entered, release := make(chan struct{}), make(chan struct{})
	RegisterResource("close_fixture", func(json.RawMessage) (interface{}, error) { close(entered); <-release; return struct{}{}, nil })
	defer RegisterResource("close_fixture", nil)
	s.state.Active.Resources = []config.Resource{{Name: "slow", Type: "close_fixture", Parameters: json.RawMessage(`{}`)}}
	started := make(chan error, 1)
	go func() { started <- s.Execute(context.Background(), "start", "", "") }()
	var err error
	require.NoError(t, err)
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	close(release)
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("shutdown did not complete")
	}
	require.Nil(t, s.lockFile)
	require.Equal(t, "stopped", s.status)
}
func TestRollbackFailureRemainsObservableAndDoesNotClaimCompletion(t *testing.T) {
	s := testSupervisor(t)
	require.NoError(t, s.Execute(context.Background(), "start", "", ""))
	err := s.Execute(context.Background(), "rollback", "", "")
	require.Error(t, err)
	require.Equal(t, "running", s.status)
}

type closeErrorResource struct{}

func (closeErrorResource) Close() error { return fmt.Errorf("injected close failure") }
func TestResourceCloseFailureIsReported(t *testing.T) {
	s := testSupervisor(t)
	RegisterResource("close_error_fixture", func(json.RawMessage) (interface{}, error) { return closeErrorResource{}, nil })
	defer RegisterResource("close_error_fixture", nil)
	s.state.Active.Resources = []config.Resource{{Name: "failure", Type: "close_error_fixture", Parameters: json.RawMessage(`{}`)}}
	require.Equal(t, "completed", runCommand(t, s, "start", "start").State)
	op := runCommand(t, s, "stop", "stop")
	require.Equal(t, "failed", op.State)
	require.Contains(t, op.Error, "injected close failure")
}

func TestServiceRemainsAvailableAfterCrawlerStartupFailure(t *testing.T) {
	for _, explicit := range []bool{true, false} {
		t.Run(fmt.Sprint("explicit-state-path-", explicit), func(t *testing.T) { serviceStartupFailure(t, explicit) })
	}
}
func serviceStartupFailure(t *testing.T, explicit bool) {
	root := t.TempDir()
	app := config.Application{Config: config.LoadConfig()}
	app.Config.Streamer.Enabled = false
	app.Config.Metrics.Enabled = false
	app.Config.API.DashDBPath = filepath.Join(root, "dash.db")
	if explicit {
		app.Config.API.OperationsPath = filepath.Join(root, "ops.json")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	app.Config.API.Address = listener.Addr().String()
	listener.Close()
	RegisterResource("startup_failure_fixture", func(json.RawMessage) (interface{}, error) { return nil, fmt.Errorf("injected startup failure") }, ProtectionMetadataOnly)
	defer RegisterResource("startup_failure_fixture", nil)
	app.Resources = []config.Resource{{Name: "failure", Type: "startup_failure_fixture", Parameters: json.RawMessage(`{}`)}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, app) }()
	client := &http.Client{Timeout: 5 * time.Second}
	base := "http://" + app.Config.API.Address
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, e := net.DialTimeout("tcp", app.Config.API.Address, 100*time.Millisecond)
		if e == nil {
			conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	request, err := http.NewRequest("POST", base+"/crawls/list", strings.NewReader(`{}`))
	require.NoError(t, err)
	res, err := client.Do(request)
	require.NoError(t, err)
	var result struct {
		Data []struct {
			Runtime struct {
				State string `json:"state"`
				Error string `json:"error"`
			} `json:"runtime"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&result))
	res.Body.Close()
	require.Equal(t, "failed", result.Data[0].Runtime.State)
	require.Contains(t, result.Data[0].Runtime.Error, "injected startup failure")
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("service did not stop")
	}
}

func TestCorruptedOperationsStateFailsWithoutStarting(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "crawls.json"), []byte(`not json`), 0600))
	w, err := NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.Error(t, err)
	require.Nil(t, w)
	require.NoError(t, os.WriteFile(filepath.Join(root, "crawls.json"), []byte(`{"schema_version":999}`), 0600))
	w, err = NewWorkspace(context.Background(), Options{DataDirectory: root})
	require.Error(t, err)
	require.Nil(t, w)
}
func TestStatusPollingDuringRepeatedRuntimeReplacement(t *testing.T) {
	s := testSupervisor(t)
	s.state.Active.Resources = []config.Resource{{Name: "requests", Type: "crawl_queue", Parameters: json.RawMessage(fmt.Sprintf(`{"path":%q,"crawl_id":"polling","max_attempts":1}`, filepath.Join(t.TempDir(), "crawl")))}}
	stop := make(chan struct{})
	var pollers sync.WaitGroup
	for i := 0; i < 6; i++ {
		pollers.Add(1)
		go func() {
			defer pollers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				s.Handle(context.Background(), "status", json.RawMessage(`{}`))
				time.Sleep(time.Millisecond)
			}
		}()
	}
	defer func() { close(stop); pollers.Wait() }()
	for i := 0; i < 15; i++ {
		require.Equal(t, "completed", runCommand(t, s, fmt.Sprintf("start-%d", i), "start").State)
		require.Equal(t, "completed", runCommand(t, s, fmt.Sprintf("stop-%d", i), "stop").State)
	}
}

func TestStartupDeadlineIsIndependentOfShutdownDeadline(t *testing.T) {
	s := testSupervisor(t)
	s.state.Active.Config.ShutdownTimeout = 10 * time.Millisecond
	s.startupTimeout = time.Second
	entered, release := make(chan struct{}), make(chan struct{})
	RegisterResource("startup_deadline_fixture", func(json.RawMessage) (interface{}, error) { close(entered); <-release; return struct{}{}, nil })
	defer RegisterResource("startup_deadline_fixture", nil)
	s.state.Active.Resources = []config.Resource{{Name: "slow", Type: "startup_deadline_fixture", Parameters: json.RawMessage(`{}`)}}
	result := make(chan error, 1)
	go func() { result <- s.Execute(context.Background(), "start", "", "") }()
	<-entered
	select {
	case e := <-result:
		t.Fatalf("startup used shutdown deadline: %v", e)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-result)
}
func TestExplicitStartupDeadlineStillExpires(t *testing.T) {
	s := testSupervisor(t)
	s.startupTimeout = 30 * time.Millisecond
	entered, release := make(chan struct{}), make(chan struct{})
	RegisterResource("startup_expires_fixture", func(json.RawMessage) (interface{}, error) { close(entered); <-release; return struct{}{}, nil })
	defer RegisterResource("startup_expires_fixture", nil)
	s.state.Active.Resources = []config.Resource{{Name: "slow", Type: "startup_expires_fixture", Parameters: json.RawMessage(`{}`)}}
	result := make(chan error, 1)
	go func() { result <- s.Execute(context.Background(), "start", "", "") }()
	<-entered
	e := <-result
	require.Error(t, e)
	require.Contains(t, e.Error(), "startup timeout")
	close(release)
}
