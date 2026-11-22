package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/protection"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/report"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/types"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (w *Workspace) protectionLoop() {
	defer w.background.Done()
	nextRefresh := time.Now()
	feedDone := make(chan struct{}, 1)
	refreshing := false
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-timer.C:
		case <-feedDone:
			refreshing = false
		}
		if w.protection.HealthError() != nil {
			if e := w.protection.Flush(); e != nil {
				continue
			}
		}

		if !refreshing && time.Now().After(nextRefresh) {
			nextRefresh = time.Now().Add(time.Minute)
			if due := w.protection.DueFeeds(time.Now()); len(due) > 0 {
				refreshing = true
				list := due[0]
				w.background.Add(1)
				go func() {
					defer w.background.Done()
					defer func() { feedDone <- struct{}{} }()
					w.refreshProtectionFeed(list)
				}()
			}
		}
		for _, incident := range w.protection.Pending() {
			if incident.State != "pending" && incident.State != "running" {
				continue
			}
			if e := w.purgeIncident(w.ctx, incident); e != nil {
				if latest, e := w.protection.PrivateIncident(incident.ID); e == nil {
					incident = latest
				}
				if incident.State == "cancelled" {
					continue
				}
				incident.State = "failed"
				incident.Error = e.Error()
				if w.ctx.Err() != nil {
					incident.State = "pending"
					incident.Error = ""
				}
				_ = w.protection.SetIncident(incident)
			}
		}
	}
}
func (w *Workspace) purgeIncident(ctx context.Context, i protection.Incident) error {
	ctx, finish, e := w.protection.PurgeContext(ctx, i.ID)
	if e != nil {
		return e
	}
	defer finish()
	i.State = "running"
	i.Error = ""
	if i.Progress == nil {
		i.Progress = map[string]string{}
	}
	if i.Progress["targets"] != "captured" {
		w.mu.Lock()
		r := w.records[i.CrawlID]
		var selected *CrawlRun
		if r != nil {
			for _, run := range r.Runs {
				if run.ID == i.RunID {
					copy := run
					selected = &copy
				}
			}
		}
		w.mu.Unlock()
		if selected == nil {
			return fmt.Errorf("detecting run unavailable")
		}
		a, e := prepareRunPipeline(w.runRoot(i.CrawlID, i.RunID), *selected)
		if e != nil {
			return e
		}
		path := a.Config.API.OperationsPath + ".report.sqlite"
		_, statErr := os.Stat(path)
		s, e := report.Open(path, os.IsNotExist(statErr))
		if e != nil {
			return e
		}
		targets, revision, partial, e := s.IncomingSites(ctx, i.URL, i.Depth)
		s.Close()
		if e != nil {
			return e
		}
		i.Targets = nil
		for _, v := range targets {
			reason := "detected_site"
			if v.Distance > 0 {
				reason = "incoming_link_depth"
			}
			i.Targets = append(i.Targets, protection.Target{Site: v.Site, Distance: v.Distance, Reason: reason})
		}
		i.Revision = revision
		i.Partial = partial
		i.Progress["targets"] = "captured"
		if e = w.protection.SetIncident(i); e != nil {
			return e
		}
	}
	i.State = "running"
	if e := w.protection.SetIncident(i); e != nil {
		return e
	}
	fenceCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	e = w.protection.WaitWrites(fenceCtx)
	cancel()
	if e != nil {
		return fmt.Errorf("purge write fence: %w", e)
	}
	matcher := protection.NewMatcher(i.Entries, i.Targets)
	match := matcher.Matches
	w.mu.Lock()
	ids := []string{}
	for id := range w.records {
		ids = append(ids, id)
	}
	w.mu.Unlock()
	for _, id := range ids {
		if e := ctx.Err(); e != nil {
			return e
		}
		w.mu.Lock()
		slot := w.slots[id]
		w.mu.Unlock()
		if slot == nil {
			continue
		}
		slot.mu.Lock()
		w.mu.Lock()
		r := w.records[id]
		if r == nil {
			w.mu.Unlock()
			slot.mu.Unlock()
			continue
		}
		runs := append([]CrawlRun(nil), r.Runs...)
		w.mu.Unlock()
		e := func() error {
			var live *preparedApplication
			var resume func()
			s := slot.supervisor
			if s != nil {
				s.log.purge(match)
				s.mu.Lock()
				if protection.References(s.lastError, match) {
					s.lastError = ""
				}
				s.mu.Unlock()
			}

			var control *runtimeControl
			if s != nil {
				s.mu.Lock()
				control = s.control
				s.mu.Unlock()
			}
			if control != nil {
				control.storageMu.RLock()
				defer control.storageMu.RUnlock()
				control.mu.Lock()
				live = control.prepared
				control.mu.Unlock()
				if live != nil {
					s.mu.Lock()
					running := s.state.Desired == "running"
					s.mu.Unlock()
					if running {
						resume = func() {
							if ctx.Err() == nil {
								_ = control.pause(ctx, false)
							}
						}
						defer resume()
						drainCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
						e := control.pause(drainCtx, true)
						cancel()
						if e != nil {
							return fmt.Errorf("purge drain: %w", e)
						}
					}
				}
			}

			for _, run := range runs {
				key := id + "/" + run.ID
				if i.Progress[key] == "complete" {
					continue
				}
				e := func() (result error) {
					a, e := prepareRunPipeline(w.runRoot(id, run.ID), run)
					if e != nil {
						return e
					}
					a.Config.HTTPClient.Protection = w.protection
					e = Validate(a)
					if e != nil {
						return e
					}
					var resources map[string]interface{}
					if live != nil && s.runID == run.ID {
						resources = live.resources
						for _, m := range live.workers {
							if p, ok := m.(interface {
								PurgeMatching(context.Context, func(string) bool) error
							}); ok {
								if e := p.PurgeMatching(ctx, match); e != nil {
									return e
								}
							}
						}
					} else {
						resources = map[string]interface{}{}
						closers := []func() error{}
						defer func() {
							for _, close := range closers {
								result = errors.Join(result, close())
							}
						}()
						for _, res := range a.Resources {
							switch res.Type {
							case "frontier_store":
								var p frontier.StoreParams
								if e = json.Unmarshal(res.Parameters, &p); e != nil {
									return e
								}
								if _, e = os.Stat(p.Path); os.IsNotExist(e) {
									continue
								}
								store, e := frontier.OpenStore(p)
								if e != nil {
									return e
								}
								resources[res.Name] = store
								closers = append(closers, func() error { return store.Close() })
							case "crawl_queue":
								var p queue.CrawlQueueParams
								if e = json.Unmarshal(res.Parameters, &p); e != nil {
									return e
								}
								if _, e = os.Stat(p.Path); os.IsNotExist(e) {
									continue
								}
								q, e := queue.NewCrawlQueue(p)
								if e != nil {
									return e
								}
								resources[res.Name] = q
								closers = append(closers, func() error {
									if c, ok := q.(io.Closer); ok {
										return c.Close()
									}
									return nil
								})
							case "file_queue":
								var p queue.FileQueueParams
								if e = json.Unmarshal(res.Parameters, &p); e != nil {
									return e
								}
								if _, e = os.Stat(p.Path); os.IsNotExist(e) {
									continue
								}
								q := queue.NewFileQueue(p)
								resources[res.Name] = q
								closers = append(closers, func() error { return q.Stop() })
							case "filesystem_object_store":
								var p objectstore.FilesystemObjectStoreParams
								if e = json.Unmarshal(res.Parameters, &p); e != nil {
									return e
								}
								if _, e = os.Stat(p.Path); e == nil {
									resources[res.Name] = objectstore.NewFilesystemObjectStore(p)
								}
							default:
								registryMutex.RLock()
								factory := resourceFactories[res.Type]
								aware := resourceProtection[res.Type] == ProtectionAware
								registryMutex.RUnlock()
								if aware {
									value, e := factory(ctx, res.Parameters, (&preparedApplication{ctx: ctx, app: a}).dependencies())
									if e != nil {
										return e
									}
									resources[res.Name] = value
									if close, ok := value.(io.Closer); ok {
										closers = append(closers, func() error { return close.Close() })
									}
									if e = checkProtectionParticipant(value, w.protection, "resource", res.Type); e != nil {
										return e
									}
									value.(ProtectionParticipant).SetProtection(w.protection)
								}
							}
						}
					}

					customWorkers := []ProtectionParticipant{}
					if live == nil || s.runID != run.ID {
						for _, wc := range a.Workers {
							registryMutex.RLock()
							factory := workerFactories[wc.Type]
							aware := workerProtection[wc.Type] == ProtectionAware
							registryMutex.RUnlock()
							if !aware {
								continue
							}
							value, e := factory(ctx, wc.Parameters, resources, (&preparedApplication{ctx: ctx, app: a}).dependencies())
							if e != nil {
								return e
							}
							defer value.OnComplete()
							if e = checkProtectionParticipant(value, w.protection, "worker", wc.Type); e != nil {
								return e
							}
							participant := value.(ProtectionParticipant)
							participant.SetProtection(w.protection)
							if e = participant.PurgeMatching(ctx, match); e != nil {
								return e
							}
							customWorkers = append(customWorkers, participant)
						}
					}
					for name, res := range resources {
						i.Progress[key+"/resource/"+name] = "purging"
						if e = w.protection.SetIncident(i); e != nil {
							return e
						}
						if p, ok := res.(interface {
							PurgeMatching(context.Context, func(string) bool) error
						}); ok {
							if e = p.PurgeMatching(ctx, match); e != nil {
								return e
							}
						}
					}
					retained := map[types.UUID]bool{}
					for _, res := range resources {
						if q, ok := res.(interface{ ReferencedObjects() ([]types.UUID, error) }); ok {
							refs, e := q.ReferencedObjects()
							if e != nil {
								return e
							}
							for _, ref := range refs {
								retained[ref] = true
							}
						}
					}
					for _, res := range resources {
						if o, ok := res.(interface {
							Reconcile(map[types.UUID]bool) error
						}); ok {
							if e = o.Reconcile(retained); e != nil {
								return e
							}
						}
					}
					for _, wc := range a.Workers {
						if wc.Type == "jsonl_sink" && (live == nil || s.runID != run.ID) {
							var p struct {
								Path string `json:"path"`
							}
							if e = json.Unmarshal(wc.Parameters, &p); e != nil {
								return e
							}
							if e = protection.RewriteJSONL(ctx, p.Path, match); e != nil {
								return e
							}
						}
					}
					for _, wc := range a.Workers {
						if wc.Type == "transformer" {
							var p struct {
								Addresses []string `json:"search_addresses"`
							}
							if e = json.Unmarshal(wc.Parameters, &p); e != nil {
								return e
							}
							if len(p.Addresses) > 0 {
								if !run.LogsScoped {
									return fmt.Errorf("legacy Elasticsearch documents require ownership migration before protection cleanup")
								}
								if e = gateway.PurgeMatchingLogs(ctx, p.Addresses, id, run.ID, match); e != nil {
									return e
								}
								i.Progress[key+"/elasticsearch/"+wc.Name] = "verified"
								if e = w.protection.SetIncident(i); e != nil {
									return e
								}
							}
						}
					}
					reportPath := a.Config.API.OperationsPath + ".report.sqlite"
					w.cancelAtlases(reportPath)
					report.Forget(reportPath)
					report.ForgetAtlas(reportPath + ".atlas.sqlite")
					if _, e = os.Stat(reportPath); e == nil {
						store, e := report.Open(reportPath, false)
						if e != nil {
							return e
						}
						if e = store.PurgeMatchingHosts(ctx, matcher.MatchesDomain); e != nil {
							store.Close()
							return e
						}
						for _, entry := range i.Entries {
							if !entry.Domain {
								if e = store.PurgeURL(ctx, entry.Value); e != nil {
									store.Close()
									return e
								}
							}
						}
						if e = store.VerifyPurged(ctx, i.Entries, i.Targets); e != nil {
							store.Close()
							return e
						}
						i.Progress[key+"/report"] = "verified"
						if e = w.protection.SetIncident(i); e != nil {
							store.Close()
							return e
						}
						if e = store.Close(); e != nil {
							return e
						}
					} else if !os.IsNotExist(e) {
						return e
					}
					if e = removeProtectionAtlasArtifacts(reportPath); e != nil {
						return e
					}
					if live != nil && s.runID == run.ID {
						s.mu.Lock()
						live.configMu.Lock()
						e = purgeApplicationSources(&live.app, match)
						if e == nil {
							e = purgeApplicationSources(&s.state.Active, match)
						}
						live.configMu.Unlock()
						s.mu.Unlock()
						if e != nil {
							return e
						}
					}

					if live != nil && s.runID == run.ID {
						for _, manager := range live.workers {
							if v, ok := manager.(interface {
								VerifyNoMatches(context.Context, func(string) bool) error
							}); ok {
								if e = v.VerifyNoMatches(ctx, match); e != nil {
									return e
								}
							}
						}
					}
					for _, participant := range customWorkers {
						if e = participant.VerifyNoMatches(ctx, match); e != nil {
							return e
						}
					}
					// Verification is separate from mutation; an incomplete cleanup must never
					// acquire a completed checkpoint.
					for name, res := range resources {
						if v, ok := res.(interface {
							VerifyNoMatches(context.Context, func(string) bool) error
						}); ok {
							if e = v.VerifyNoMatches(ctx, match); e != nil {
								return e
							}
						}
						i.Progress[key+"/resource/"+name] = "verified"
						if e = w.protection.SetIncident(i); e != nil {
							return e
						}
					}
					for _, wc := range a.Workers {
						if wc.Type == "jsonl_sink" {
							var p struct {
								Path string `json:"path"`
							}
							if e = json.Unmarshal(wc.Parameters, &p); e != nil {
								return e
							}
							if e = protection.VerifyJSONL(ctx, p.Path, match); e != nil {
								return e
							}
							i.Progress[key+"/results/"+wc.Name] = "verified"
							if e = w.protection.SetIncident(i); e != nil {
								return e
							}
						}
					}

					for _, path := range []string{a.Config.API.OperationsPath, a.Config.API.OperationsPath + ".tmp"} {
						raw, readError := os.ReadFile(path)
						if os.IsNotExist(readError) {
							continue
						}
						if readError != nil {
							return readError
						}
						clean, e := purgeConfigurationJSON(raw, match)
						if e != nil {
							return e
						}
						if !bytes.Equal(raw, clean) {
							if e = protection.ReplaceFile(ctx, path, clean); e != nil {
								return e
							}
						}
					}
					return nil
				}()
				if e != nil {
					return e
				}
				i.Progress[key] = "complete"
				if e = w.protection.SetIncident(i); e != nil {
					return e
				}
			}
			return nil
		}()
		slot.mu.Unlock()
		if e != nil {
			return e
		}
	}
	// Registry snapshots can contain seed URLs and custom pipeline parameters.
	w.mu.Lock()
	for _, r := range w.records {
		if e := purgeRecordSources(r, match); e != nil {
			w.mu.Unlock()
			return e
		}
	}

	e = w.saveLocked()
	w.mu.Unlock()
	if e != nil {
		return e
	}
	// Private migration copies are owned by this application; sanitize JSON files.
	e = filepath.WalkDir(filepath.Join(w.root, "migration-backups"), func(path string, d os.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeType != 0 {
			return fmt.Errorf("unsupported configuration backup file")
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		clean, e := purgeBackupJSON(raw, match)
		if e != nil {
			return fmt.Errorf("configuration backup %s: %w", filepath.Base(path), e)
		}
		if !bytes.Equal(bytes.TrimSpace(raw), bytes.TrimSpace(clean)) {
			return protection.ReplaceFile(ctx, path, clean)
		}

		return nil
	})
	if e != nil {
		return e
	}
	i.Entries = nil
	i.State = "completed"
	return w.protection.SetIncident(i)
}

// A slow feed download never holds up incident propagation. Only one scheduled
// download runs at a time, and stale downloads cannot overwrite an edited list.
func (w *Workspace) refreshProtectionFeed(list protection.List) {
	preview, e := protection.PreviewList(w.ctx, &http.Client{Timeout: 30 * time.Second}, list, &list)
	if e != nil {
		_ = w.protection.RecordFeedError(list.ID, list.URL, e.Error())
		return
	}
	current := w.protection.Configuration()
	lists := append([]protection.List{}, current.Lists...)
	found := false
	for n, l := range lists {
		if l.ID == list.ID && l.URL == list.URL && l.Version == list.Version && l.Enabled {
			preview.List.Name = l.Name
			preview.List.Refresh = l.Refresh
			preview.List.Enabled = l.Enabled
			lists[n] = preview.List
			found = true
			break
		}
	}
	if !found {
		return
	}
	if preview.List.Version == list.Version {
		_ = w.protection.Update(current.Revision, lists, current.Rules)
	} else {
		_ = w.protection.Activate(current.Revision, lists, current.Rules, preview.List)
	}
}

// Called only after atlas generators have stopped; remove completed and interrupted snapshots.
func removeProtectionAtlasArtifacts(reportPath string) error {
	prefix := filepath.Base(reportPath) + ".atlas.sqlite"
	dir := filepath.Dir(reportPath)
	f, err := os.Open(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	for {
		entries, err := f.ReadDir(128)
		if err != nil && err != io.EOF {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if name == prefix || name == prefix+"-wal" || name == prefix+"-shm" || (strings.HasPrefix(name, prefix+".") && (strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".tmp-journal"))) {
				if e := os.Remove(filepath.Join(dir, name)); e != nil && !os.IsNotExist(e) {
					return e
				}
			}
		}
		if err == io.EOF {
			break
		}
	}
	return f.Sync()
}
