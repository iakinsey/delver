package gateway

import "database/sql"

func migrateDashboard(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(createDashboard); err != nil {
		return err
	}
	rows, err := tx.Query("PRAGMA table_info(dashboard)")
	if err != nil {
		return err
	}
	legacy := false
	for rows.Next() {
		var id int
		var name, kind string
		var required, primary int
		var value interface{}
		if err = rows.Scan(&id, &name, &kind, &required, &value, &primary); err != nil {
			rows.Close()
			return err
		}
		legacy = legacy || name == "user_id"
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if _, err = tx.Exec("CREATE TABLE IF NOT EXISTS application_metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL)"); err != nil {
		return err
	}
	if legacy {
		for _, query := range []string{"ALTER TABLE dashboard RENAME TO dashboard_legacy", createDashboard, "INSERT INTO dashboard(id,name,value,description) SELECT id,name,value,description FROM dashboard_legacy", "DROP TABLE dashboard_legacy", "INSERT OR IGNORE INTO application_metadata(key,value) VALUES ('default_initialized','1')"} {
			if _, err = tx.Exec(query); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec("DROP TABLE IF EXISTS dashboard_defaults"); err != nil {
		return err
	}
	return tx.Commit()
}
