package protection

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func entryVersion(entries []Entry) string {
	h := sha256.New()
	encoder := json.NewEncoder(h)
	for _, entry := range entries {
		_ = encoder.Encode(entry)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func (p *Policy) loadStorage() error {
	_, e := p.db.Exec(`CREATE TABLE IF NOT EXISTS metadata(kind TEXT NOT NULL,id TEXT NOT NULL,value TEXT NOT NULL,PRIMARY KEY(kind,id)) WITHOUT ROWID;CREATE TABLE IF NOT EXISTS entries(owner TEXT NOT NULL,id TEXT NOT NULL,ordinal INTEGER NOT NULL,domain INTEGER NOT NULL,value TEXT NOT NULL,PRIMARY KEY(owner,id,ordinal)) WITHOUT ROWID`)
	if e != nil {
		return e
	}
	rows, e := p.db.Query(`SELECT kind,id,value FROM metadata ORDER BY kind,id`)
	if e != nil {
		return e
	}
	found := false
	for rows.Next() {
		var kind, id, raw string
		if e = rows.Scan(&kind, &id, &raw); e != nil {
			rows.Close()
			return e
		}
		p.persistedRows[kind+":"+id] = raw
		switch kind {
		case "settings":
			found = true
			var settings struct {
				Revision      uint64 `json:"revision"`
				InspectionMiB int    `json:"inspection_memory_mib"`
				Rules         []Rule `json:"rules"`
			}
			if e = json.Unmarshal([]byte(raw), &settings); e != nil {
				rows.Close()
				return e
			}
			p.state.Revision = settings.Revision
			p.state.InspectionMiB = settings.InspectionMiB
			p.state.Rules = settings.Rules
		case "list":
			var l List
			if e = json.Unmarshal([]byte(raw), &l); e != nil {
				rows.Close()
				return e
			}
			l.Entries = []Entry{}
			p.state.Lists = append(p.state.Lists, l)
			p.listVersions[id] = l.Version
		case "incident":
			var i Incident
			if e = json.Unmarshal([]byte(raw), &i); e != nil {
				rows.Close()
				return e
			}
			p.state.Incidents = append(p.state.Incidents, i)
		default:
			rows.Close()
			return fmt.Errorf("unknown protection metadata kind %q", kind)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if !found {
		var old string
		e = p.db.QueryRow(`SELECT value FROM policy WHERE id=1`).Scan(&old)
		if e == nil {
			if e = json.Unmarshal([]byte(old), &p.state); e != nil {
				return e
			}
		} else if e != sql.ErrNoRows {
			var exists int
			if x := p.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='policy'`).Scan(&exists); x != nil {
				return x
			}
			if exists != 0 {
				return e
			}
		}
		return nil
	}
	lists := map[string]int{}
	for n, l := range p.state.Lists {
		lists[l.ID] = n
	}
	jobs := map[string]int{}
	for n, i := range p.state.Incidents {
		jobs[i.ID] = n
	}
	rows, e = p.db.Query(`SELECT owner,id,domain,value FROM entries ORDER BY owner,id,ordinal`)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var owner, id string
		var entry Entry
		if e = rows.Scan(&owner, &id, &entry.Domain, &entry.Value); e != nil {
			return e
		}
		switch owner {
		case "list":
			n, ok := lists[id]
			if !ok {
				return fmt.Errorf("orphan blocklist entries")
			}
			p.state.Lists[n].Entries = append(p.state.Lists[n].Entries, entry)
		case "incident":
			n, ok := jobs[id]
			if !ok {
				return fmt.Errorf("orphan purge entries")
			}
			p.state.Incidents[n].Entries = append(p.state.Incidents[n].Entries, entry)
			p.jobEntries[id] = true
		default:
			return fmt.Errorf("unknown protection entry owner")
		}
	}
	return rows.Err()
}
func (p *Policy) persistLocked() (result error) {
	defer func() {
		p.storeError = result
		if result != nil {
			p.cancelLocked()
		}
		p.current.Store(&policyIndex{p.domains, p.urls, p.quarantines, result != nil})
		if p.healthChanged != nil {
			close(p.healthChanged)
		}
		p.healthChanged = make(chan struct{})
	}()
	tx, e := p.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	nextRows := map[string]string{}
	nextVersions := map[string]string{}
	nextJobs := map[string]bool{}
	put := func(kind, id string, value interface{}) error {
		raw, e := json.Marshal(value)
		if e != nil {
			return e
		}
		key := kind + ":" + id
		nextRows[key] = string(raw)
		if p.persistedRows[key] == string(raw) {
			return nil
		}
		_, e = tx.Exec(`INSERT INTO metadata(kind,id,value) VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET value=excluded.value`, kind, id, string(raw))
		return e
	}
	entries := func(owner, id string, values []Entry) error {
		if _, e := tx.Exec(`DELETE FROM entries WHERE owner=? AND id=?`, owner, id); e != nil {
			return e
		}
		stmt, e := tx.Prepare(`INSERT INTO entries(owner,id,ordinal,domain,value) VALUES(?,?,?,?,?)`)
		if e != nil {
			return e
		}
		defer stmt.Close()
		for n, v := range values {
			if _, e = stmt.Exec(owner, id, n, v.Domain, v.Value); e != nil {
				return e
			}
		}
		return nil
	}
	settings := struct {
		Revision      uint64 `json:"revision"`
		InspectionMiB int    `json:"inspection_memory_mib"`
		Rules         []Rule `json:"rules"`
	}{p.state.Revision, p.state.InspectionMiB, p.state.Rules}
	if e = put("settings", "workspace", settings); e != nil {
		return e
	}
	for _, list := range p.state.Lists {
		metadata := list
		metadata.Entries = nil
		if metadata.Version == "" {
			metadata.Version = entryVersion(list.Entries)
		}
		if e = put("list", list.ID, metadata); e != nil {
			return e
		}
		nextVersions[list.ID] = metadata.Version
		if p.listVersions[list.ID] != metadata.Version {
			if e = entries("list", list.ID, list.Entries); e != nil {
				return e
			}
		}
	}
	for _, incident := range p.state.Incidents {
		metadata := incident
		metadata.Entries = nil
		if e = put("incident", incident.ID, metadata); e != nil {
			return e
		}
		if len(incident.Entries) > 0 {
			nextJobs[incident.ID] = true
			if !p.jobEntries[incident.ID] {
				if e = entries("incident", incident.ID, incident.Entries); e != nil {
					return e
				}
			}
		} else if p.jobEntries[incident.ID] {
			if e = entries("incident", incident.ID, nil); e != nil {
				return e
			}
		}
	}
	for key := range p.persistedRows {
		if _, ok := nextRows[key]; !ok {
			var kind, id string
			for n, c := range key {
				if c == ':' {
					kind, id = key[:n], key[n+1:]
					break
				}
			}
			if _, e = tx.Exec(`DELETE FROM metadata WHERE kind=? AND id=?`, kind, id); e != nil {
				return e
			}
			if _, e = tx.Exec(`DELETE FROM entries WHERE owner=? AND id=?`, kind, id); e != nil {
				return e
			}
		}
	}
	if _, e = tx.Exec(`DROP TABLE IF EXISTS policy`); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	p.persistedRows = nextRows
	p.listVersions = nextVersions
	p.jobEntries = nextJobs
	return nil
}
