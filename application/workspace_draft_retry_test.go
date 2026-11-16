package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkspaceDraftRetryPreservesVersionAcrossRestartAndLaterMutations(t *testing.T) {
	options := Options{DataDirectory: t.TempDir()}
	workspace, err := NewWorkspace(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	definition := DefaultCrawlDefinition()
	definition.Seeds = []string{"https://example.test/"}
	record := createWorkspaceCrawl(t, workspace, definition)
	definition.Name = "saved draft"
	request := map[string]interface{}{"crawl_id": record["id"], "revision": record["revision"], "id": "draft-retry", "definition": definition}
	saved := workspaceCall(t, workspace, "draft", request).(map[string]interface{})
	version := saved["revision"]
	if _, ok := saved["draft_hash"]; ok {
		t.Fatal("private draft digest leaked")
	}
	workspace.Close()
	workspace, err = NewWorkspace(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	retried := workspaceCall(t, workspace, "draft", request).(map[string]interface{})
	if retried["revision"] != version {
		t.Fatal("retry incremented draft version", retried)
	}
	raw, _ := json.Marshal(request)
	changed := strings.Replace(string(raw), "saved draft", "different payload", 1)
	if _, err := workspace.HandleCrawls(context.Background(), "draft", []byte(changed)); err == nil {
		t.Fatal("draft ID accepted different content")
	}
	workspaceCall(t, workspace, "archive", map[string]interface{}{"crawl_id": record["id"], "revision": version})
	retried = workspaceCall(t, workspace, "draft", request).(map[string]interface{})
	if retried["revision"] != version {
		t.Fatal("retry returned a newer mutation version", retried)
	}
	current := workspaceCall(t, workspace, "detail", map[string]interface{}{"crawl_id": record["id"]}).(map[string]interface{})
	if current["revision"] == version {
		t.Fatal("expected archive to have advanced actual revision")
	}
}
