package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/report"
	"net/http"
	"os"
	"strings"
	"time"
)

type protectionPreview struct {
	Preview  protection.Preview
	Revision uint64
	Expires  time.Time
}

func protectionSummary(s protection.State) map[string]interface{} {
	lists := []map[string]interface{}{}
	for _, l := range s.Lists {
		lists = append(lists, map[string]interface{}{"id": l.ID, "name": l.Name, "enabled": l.Enabled, "url": l.URL, "refresh": l.Refresh, "updated": l.Updated, "error": l.Error, "entries": max(l.EntryCount, len(l.Entries))})
	}
	for n := range s.Incidents {
		s.Incidents[n].Entries = nil
	}
	return map[string]interface{}{"health": s.Health, "revision": s.Revision, "inspection_memory_mib": s.InspectionMiB, "lists": lists, "rules": s.Rules, "incidents": s.Incidents}
}
func (w *Workspace) handleProtection(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var r struct {
		Action    string            `json:"action"`
		Revision  uint64            `json:"revision"`
		ID        string            `json:"id"`
		Token     string            `json:"token"`
		List      protection.List   `json:"list"`
		Rules     []protection.Rule `json:"rules"`
		Confirmed bool              `json:"confirmed"`
		MemoryMiB int               `json:"inspection_memory_mib"`
		Offset    int               `json:"offset"`
		Query     string            `json:"query"`
	}
	if e := json.Unmarshal(raw, &r); e != nil {
		return nil, e
	}
	if r.Action == "" || r.Action == "state" {
		return protectionSummary(w.protection.PublicSnapshot()), nil
	}
	if r.Action == "incident" {
		return w.protection.Incident(r.ID, r.Offset, 200)
	}
	if r.Action == "incidents" {
		return w.protection.SearchIncidents(r.Query), nil
	}
	s := w.protection.Configuration()
	switch r.Action {
	case "", "state":
		return protectionSummary(s), nil
	case "match":
		return map[string]interface{}{"sources": w.protection.Match(r.Query)}, nil
	case "list":
		for _, l := range s.Lists {
			if l.ID == r.ID {
				l.Entries = nil
				return l, nil
			}
		}
		return nil, fmt.Errorf("list not found")
	case "preview":
		if r.Revision != s.Revision {
			return nil, fmt.Errorf("protection revision conflict")
		}
		var old *protection.List
		for _, l := range s.Lists {
			if l.ID == r.List.ID {
				copy := l
				old = &copy
			}
		}
		if r.List.ID == "" {
			r.List.ID = uuid.NewString()
		}
		if strings.TrimSpace(r.List.Name) == "" {
			return nil, fmt.Errorf("list name required")
		}
		loadError := ""
		preview, e := protection.PreviewList(ctx, &http.Client{Timeout: 30 * time.Second}, r.List, old)
		if e != nil {
			if old != nil && old.URL != "" {
				_ = w.protection.RecordFeedError(old.ID, old.URL, e.Error())
			}
			if old != nil || r.List.URL == "" {
				return nil, e
			}
			if _, validation := protection.NormalizeDestination(r.List.URL); validation != nil {
				return nil, validation
			}
			loadError = e.Error()
			list := r.List
			list.Entries = nil
			list.Updated = time.Time{}
			list.Attempted = time.Now().UTC()
			list.Error = loadError
			if list.Refresh == "" {
				list.Refresh = "6h"
			}
			preview = protection.Preview{List: list}
		}
		affected := report.MatchingCounts{}
		ledgers := 0
		w.mu.Lock()
		encoded, e := json.Marshal(w.records)
		var snapshot map[string]*CrawlRecord
		if e == nil {
			e = json.Unmarshal(encoded, &snapshot)
		}
		records := []CrawlRecord{}
		for _, record := range snapshot {
			records = append(records, *record)
		}
		w.mu.Unlock()
		if e != nil {
			return nil, e
		}
		for _, record := range records {
			for _, run := range record.Runs {
				app, e := prepareRunPipeline(w.runRoot(record.ID, run.ID), run)
				if e != nil {
					return nil, e
				}
				path := app.Config.API.OperationsPath + ".report.sqlite"
				if _, e = os.Stat(path); os.IsNotExist(e) {
					continue
				}
				ledger, e := report.Open(path, false)
				if e != nil {
					return nil, e
				}
				counts, e := ledger.MatchingCounts(ctx, preview.List.Entries)
				ledger.Close()
				if e != nil {
					return nil, e
				}
				ledgers++
				affected.URLs += counts.URLs
				affected.Hosts += counts.Hosts
				affected.Partial = affected.Partial || counts.Partial
			}
		}
		token := uuid.NewString()
		w.mu.Lock()
		if w.protectionPreviews == nil {
			w.protectionPreviews = map[string]protectionPreview{}
		}
		for k, v := range w.protectionPreviews {
			if time.Now().After(v.Expires) {
				delete(w.protectionPreviews, k)
			}
		}
		for len(w.protectionPreviews) >= 4 {
			oldest := ""
			var expires time.Time
			for key, value := range w.protectionPreviews {
				if oldest == "" || value.Expires.Before(expires) {
					oldest = key
					expires = value.Expires
				}
			}
			delete(w.protectionPreviews, oldest)
		}
		w.protectionPreviews[token] = protectionPreview{preview, s.Revision, time.Now().Add(15 * time.Minute)}
		w.mu.Unlock()
		return map[string]interface{}{"token": token, "added": preview.Added, "removed": preview.Removed, "entries": len(preview.List.Entries), "list": map[string]interface{}{"id": preview.List.ID, "name": preview.List.Name, "enabled": preview.List.Enabled}, "purges_existing_data": preview.List.Enabled && !preview.List.Updated.IsZero(), "load_error": loadError, "recorded_matches": affected, "ledgers": ledgers}, nil
	case "apply":
		if !r.Confirmed {
			return nil, fmt.Errorf("activation confirmation required")
		}
		for _, list := range s.Lists {
			if list.ActivationID == r.Token && list.ActivationID != "" && list.ActivationRevision == r.Revision {
				return protectionSummary(w.protection.PublicSnapshot()), nil
			}
		}
		w.mu.Lock()
		v, ok := w.protectionPreviews[r.Token]
		if ok {
			delete(w.protectionPreviews, r.Token)
		}
		w.mu.Unlock()
		if !ok || time.Now().After(v.Expires) {
			return nil, fmt.Errorf("preview expired")
		}
		if r.Revision != v.Revision || s.Revision != v.Revision {
			return nil, fmt.Errorf("protection revision conflict")
		}
		next := []protection.List{}
		for _, l := range s.Lists {
			if l.ID != v.Preview.List.ID {
				next = append(next, l)
			}
		}
		v.Preview.List.ActivationID = r.Token
		v.Preview.List.ActivationRevision = r.Revision
		next = append(next, v.Preview.List)
		if e := w.protection.Activate(s.Revision, next, s.Rules, v.Preview.List); e != nil {
			return nil, e
		}
		return protectionSummary(w.protection.PublicSnapshot()), nil
	case "disable", "delete":
		next := []protection.List{}
		found := false
		for _, l := range s.Lists {
			if l.ID == r.ID {
				found = true
				if r.Action == "delete" {
					continue
				}
				l.Enabled = false
			}
			next = append(next, l)
		}
		if !found {
			return nil, fmt.Errorf("list not found")
		}
		if e := w.protection.Update(r.Revision, next, s.Rules); e != nil {
			return nil, e
		}
		return protectionSummary(w.protection.PublicSnapshot()), nil
	case "memory":

		w.mu.Lock()
		for _, record := range w.records {
			if len(record.Runs) == 0 {
				continue
			}
			run := record.Runs[len(record.Runs)-1]
			app, e := prepareRunPipeline(w.runRoot(record.ID, run.ID), run)
			if e != nil {
				w.mu.Unlock()
				return nil, e
			}
			if !protection.FitsResponse(app.Config.HTTPClient.MaxResponseBytes, r.MemoryMiB) {
				w.mu.Unlock()
				return nil, fmt.Errorf("inspection memory budget cannot hold configured response limit for crawl %s", record.ID)
			}
		}
		w.mu.Unlock()
		if e := w.protection.SetMemory(r.Revision, r.MemoryMiB); e != nil {
			return nil, e
		}
		return protectionSummary(w.protection.PublicSnapshot()), nil
	case "rules":
		if e := w.protection.Update(r.Revision, s.Lists, r.Rules); e != nil {
			return nil, e
		}
		return protectionSummary(w.protection.PublicSnapshot()), nil
	case "retry":
		incident, e := w.protection.PrivateIncident(r.ID)
		if e != nil {
			return nil, e
		}
		if incident.State != "failed" {
			return nil, fmt.Errorf("failed incident not found")
		}
		incident.State = "pending"
		incident.Error = ""
		if e = w.protection.SetIncident(incident); e != nil {
			return nil, e
		}
		return protectionSummary(w.protection.PublicSnapshot()), nil
	case "export":
		return w.protection.Incident(r.ID, 0, int(^uint(0)>>1))
	default:
		return nil, fmt.Errorf("unknown protection action")
	}
}

func (w *Workspace) ProtectionReport(ctx context.Context, id string) (interface{}, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	return w.protection.Incident(id, 0, int(^uint(0)>>1))
}
