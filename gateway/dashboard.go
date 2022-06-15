package gateway

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/errs"
	log "github.com/sirupsen/logrus"
)

var NoDashErr = errs.NewDashError("Dashboard does not exist")

var createDashboard = `
    CREATE TABLE IF NOT EXISTS dashboard (
        id STRING PRIMARY KEY,
        name STRING NOT NULL,
        user_id STRING NOT NULL,
        value BLOB NOT NULL,
        description STRING
    )
`

var getDashboard = `
    SELECT id, name, user_id, value, description FROM dashboard WHERE id = ?
    ORDER BY name asc LIMIT 1
`

var putDashboard = `
    REPLACE INTO dashboard (id, name, user_id, value, description) VALUES (?, ?, ?, ?, ?)
`

var deleteDashboard = `
    DELETE FROM dashboard WHERE id = ? and user_id = ?
`

var getUserDashboards = `
    SELECT id, name, user_id, value, description FROM dashboard WHERE user_id = ?
`

type DashboardGateway interface {
	Put(dash types.Dashboard) error
	Get(userID, dashID string) (*types.Dashboard, error)
	Delete(userID, dashID string) error
	List(userID string) ([]types.Dashboard, error)
}

type dashboardGateway struct {
	db *sql.DB
}

func NewDashboardGateway(path string) DashboardGateway {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s", path))

	if err != nil {
		log.Fatalf("failed to open dashboard sqlite database %s", err)
	}

	g := &dashboardGateway{
		db: db,
	}

	g.setupDashDb()

	return g
}

func (s *dashboardGateway) setupDashDb() {
	if _, err := s.db.Exec(createDashboard); err != nil {
		log.Fatalf("failed to setup dashboard database: %s", err)
	}
}

func (s *dashboardGateway) Put(d types.Dashboard) error {
	if err := s.isAuthorized(d.UserID, d.ID); err != nil {
		return err
	}

	_, err := s.db.Exec(putDashboard, d.ID, d.Name, d.UserID, d.Value, d.Description)

	return err
}

func (s *dashboardGateway) Get(userID string, dashID string) (*types.Dashboard, error) {
	row := s.db.QueryRow(getDashboard, dashID)

	if row.Err() != nil {
		return nil, row.Err()
	}

	d := &types.Dashboard{}
	var val []byte

	if err := row.Scan(&d.ID, &d.Name, &d.UserID, &val, &d.Description); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, NoDashErr
		}

		return nil, err
	}

	if d.UserID != userID {
		return nil, errs.NewDashError("Unauthorized")
	}

	if err := d.Value.UnmarshalJSON(val); err != nil {
		return nil, err
	}

	return d, nil
}

func (s *dashboardGateway) Delete(userID string, dashID string) error {
	if _, err := s.Get(userID, dashID); err != nil {
		return err
	}

	_, err := s.db.Exec(deleteDashboard, dashID, userID)

	return err
}

func (s *dashboardGateway) List(userID string) ([]types.Dashboard, error) {
	rows, err := s.db.Query(getUserDashboards, userID)

	if err != nil {
		return nil, err
	}

	var result []types.Dashboard

	for rows.Next() {
		d := types.Dashboard{}
		var val []byte

		if err := rows.Scan(&d.ID, &d.Name, &d.UserID, &val, &d.Description); err != nil {
			return nil, err
		}

		if err := d.Value.UnmarshalJSON(val); err != nil {
			return nil, err
		}

		result = append(result, d)
	}

	return result, nil
}

func (s *dashboardGateway) isAuthorized(userID, dashID string) error {
	_, err := s.Get(userID, dashID)

	if errors.Is(err, NoDashErr) {
		return nil
	}

	return err
}
