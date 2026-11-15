package gateway

import (
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/mattn/go-sqlite3"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/errs"
)

var NoDashErr = errs.NewDashError("Dashboard does not exist")

var createDashboard = `
    CREATE TABLE IF NOT EXISTS dashboard (
        id STRING PRIMARY KEY,
        name STRING NOT NULL,
        value BLOB NOT NULL,
        description STRING
    )
`

var getDashboard = `
    SELECT id, name, value, description FROM dashboard WHERE id = ?
    ORDER BY name asc LIMIT 1
`

var putDashboard = `
    REPLACE INTO dashboard (id, name, value, description) VALUES (?, ?, ?, ?)
`

var deleteDashboard = `
    DELETE FROM dashboard WHERE id = ?
`

var getDashboards = `
    SELECT id, name, value, description FROM dashboard ORDER BY name, id
`

type DashboardGateway interface {
	EnsureDefault() error
	Put(dash types.Dashboard) error
	Get(dashID string) (*types.Dashboard, error)
	Delete(dashID string) error
	List() ([]types.Dashboard, error)
}

type dashboardGateway struct {
	db *sql.DB
}

func NewDashboardGateway(path string) DashboardGateway {
	gateway, err := OpenDashboardGateway(path)
	if err != nil {
		panic(err)
	}
	return gateway
}
func OpenDashboardGateway(path string) (DashboardGateway, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_busy_timeout=5000", path))
	if err != nil {
		return nil, err
	}
	if err := migrateDashboard(db); err != nil {
		db.Close()
		return nil, err
	}

	return &dashboardGateway{db: db}, nil
}

func (s *dashboardGateway) Put(d types.Dashboard) error {

	_, err := s.db.Exec(putDashboard, d.ID, d.Name, d.Value, d.Description)

	return err
}

func (s *dashboardGateway) Get(dashID string) (*types.Dashboard, error) {
	row := s.db.QueryRow(getDashboard, dashID)

	if row.Err() != nil {
		return nil, row.Err()
	}

	d := &types.Dashboard{}
	var val []byte

	if err := row.Scan(&d.ID, &d.Name, &val, &d.Description); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, NoDashErr
		}

		return nil, err
	}

	if err := d.Value.UnmarshalJSON(val); err != nil {
		return nil, err
	}

	return d, nil
}

func (s *dashboardGateway) Delete(dashID string) error {
	if _, err := s.Get(dashID); err != nil {
		return err
	}

	_, err := s.db.Exec(deleteDashboard, dashID)

	return err
}

func (s *dashboardGateway) List() ([]types.Dashboard, error) {
	rows, err := s.db.Query(getDashboards)

	if err != nil {
		return nil, err
	}

	defer rows.Close()
	var result []types.Dashboard

	for rows.Next() {
		d := types.Dashboard{}
		var val []byte

		if err := rows.Scan(&d.ID, &d.Name, &val, &d.Description); err != nil {
			return nil, err
		}

		if err := d.Value.UnmarshalJSON(val); err != nil {
			return nil, err
		}

		result = append(result, d)
	}

	return result, rows.Err()
}

func (s *dashboardGateway) Close() error { return s.db.Close() }
