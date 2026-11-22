package main

// Synthetic graph loading is confined to this local test executable. Production
// API routes never expose fixture injection or accept storage paths.
import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/application"
	"github.com/iakinsey/delver/report"
	"net/http"
	"path/filepath"
	"time"
)

func atlasFixture(root string, workspace *application.Workspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		var q struct {
			CrawlID string `json:"crawl_id"`
			Count   int    `json:"count"`
		}
		if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&q); e != nil || q.Count < 10 || q.Count > 1000000 {
			http.Error(w, "invalid fixture", 400)
			return
		}
		raw, _ := json.Marshal(map[string]string{"crawl_id": q.CrawlID})
		v, e := workspace.HandleCrawls(r.Context(), "detail", raw)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		b, _ := json.Marshal(v)
		var record struct {
			ID   string `json:"id"`
			Runs []struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"runs"`
		}
		json.Unmarshal(b, &record)
		if len(record.Runs) == 0 || record.Runs[len(record.Runs)-1].State != "completed" {
			http.Error(w, "completed fixture crawl required", 400)
			return
		}
		path := filepath.Join(root, "crawls", record.ID, "runs", record.Runs[len(record.Runs)-1].ID, "state.json.report.sqlite")
		store, e := report.Open(path, false)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		store.Close()
		// Loading a million synthetic rows can exceed the production API's 30s
		// response deadline under a parallel browser run. Extend only this test
		// endpoint; production routes retain their normal deadlines.
		if e = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute)); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		e = seedAtlasFixture(r.Context(), path, q.Count)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]int{"hosts_added": q.Count})
	}
}
func seedAtlasFixture(ctx context.Context, path string, count int) error {
	db, e := sql.Open("sqlite3", "file:"+path+"?_busy_timeout=5000")
	if e != nil {
		return e
	}
	defer db.Close()
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var base int
	if e = tx.QueryRow("SELECT coalesce(max(id),0)+1 FROM hosts").Scan(&base); e != nil {
		return e
	}
	ns, e := tx.PrepareContext(ctx, "INSERT INTO hosts(id,name,known,visited) VALUES(?,?,1,1)")
	if e != nil {
		return e
	}
	defer ns.Close()
	es, e := tx.PrepareContext(ctx, "INSERT INTO edges VALUES(?,?)")
	if e != nil {
		return e
	}
	defer es.Close()
	for i := 0; i < count; i++ {
		id := base + i
		if _, e = ns.ExecContext(ctx, id, fmt.Sprintf("scale-%07d.test", i)); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO site_hosts(host,site) VALUES(?,?)", id, fmt.Sprintf("scale-%07d.test", i)); e != nil {
			return e
		}
		if i > 0 {
			if _, e = es.ExecContext(ctx, id-1, id); e != nil {
				return e
			}
		}
		if i > 0 && i%1000 == 0 {
			if _, e = es.ExecContext(ctx, base, id); e != nil {
				return e
			}
		}
		if i > 10 {
			if _, e = es.ExecContext(ctx, id, id-10); e != nil {
				return e
			}
		}
	}
	_, e = tx.ExecContext(ctx, "UPDATE meta SET host_count=(SELECT count(*) FROM hosts),edge_count=(SELECT count(*) FROM edges),known=known+?,visited=visited+?,revision=revision+1", count, count)
	if e != nil {
		return e
	}
	return tx.Commit()
}
