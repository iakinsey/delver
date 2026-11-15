package gateway

import (
	"database/sql"
	"encoding/json"
)

func migrateDefaultCrawlLog(tx *sql.Tx) error {
	result, err := tx.Exec("INSERT OR IGNORE INTO application_metadata(key,value) VALUES ('crawl_log_default_v1','1')")
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	rows, err := tx.Query("SELECT id,value FROM dashboard")
	if err != nil {
		return err
	}
	type update struct {
		id    string
		value []byte
	}
	var changes []update
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var dashboard map[string]json.RawMessage
		if err = json.Unmarshal(raw, &dashboard); err != nil {
			rows.Close()
			return err
		}
		var cells []map[string]json.RawMessage
		if err = json.Unmarshal(dashboard["cells"], &cells); err != nil {
			continue
		}
		changed := false
		for _, cell := range cells {
			var title, kind string
			json.Unmarshal(cell["title"], &title)
			json.Unmarshal(cell["type"], &kind)
			if title == "Latest headlines" && kind == "entityFeed" {
				cell["title"] = json.RawMessage(`"Crawl log"`)
				cell["type"] = json.RawMessage(`"crawlLog"`)
				delete(cell, "filter")
				changed = true
			}
		}
		if changed {
			dashboard["cells"], err = json.Marshal(cells)
			if err != nil {
				rows.Close()
				return err
			}
			value, e := json.Marshal(dashboard)
			if e != nil {
				rows.Close()
				return e
			}
			changes = append(changes, update{id, value})
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, change := range changes {
		if _, err = tx.Exec("UPDATE dashboard SET value=? WHERE id=?", change.value, change.id); err != nil {
			return err
		}
	}
	return nil
}
