package gateway

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/iakinsey/delver/types/instrument"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type metricSqlite struct {
	db *sql.DB
}

func NewMetricSqlite(path string) MetricsGateway {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s", path))

	if err != nil {
		log.Fatalf("failed to open metric sqlite database %s", err)
	}

	return &metricSqlite{
		db: db,
	}
}

func (s *metricSqlite) declareMetric(n string) {
	d := []string{
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (ts INTEGER NOT NULL, value INTEGER NOT NULL)", n),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s_ts_idx ON %s (ts ASC)", n, n),
	}

	for _, q := range d {
		if _, err := s.db.Exec(q); err != nil {
			log.Fatalf("failed to declare metric %s: %s", n, err)
		}
	}

}

func (s *metricSqlite) Get(query instrument.MetricsQuery) ([]instrument.Metric, error) {
	if query.End == 0 {
		query.End = math.MaxInt64
	}

	q := fmt.Sprintf(`
		SELECT 
		ts,
		value
		FROM %s
		ORDER BY ts ASC
	`, escapeMetricName(query.Key))

	rows, err := s.db.Query(q) //, query.Start, query.End)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	if query.Agg == "" {
		return processNoAgg(rows), nil
	}

	return processWithAgg(query, rows), nil
}

func (s *metricSqlite) List() (result []string, err error) {
	rows, err := s.db.Query(`
		SELECT
		name
		FROM
		sqlite_schema
		WHERE type ='table'
		AND name NOT LIKE 'sqlite_%'
	`)

	if err != nil {
		return
	}

	defer rows.Close()

	for rows.Next() {
		var name string

		if err := rows.Scan(&name); err != nil {
			log.Fatal(err)
		}

		result = append(result, unescapeMetricName(name))
	}

	return
}

func processWithAgg(query instrument.MetricsQuery, rows *sql.Rows) (metrics []instrument.Metric) {
	var aggWindow []int64
	var result []instrument.Metric
	var aggStart int64 = -1

	for rows.Next() {
		var (
			when  int64
			value int64
		)

		if err := rows.Scan(&when, &value); err != nil {
			log.Fatal(err)
		}

		aggWindow = append(aggWindow, value)

		if aggStart == -1 {
			aggStart = when
		}

		if when-aggStart >= query.Window {
			result = append(result, instrument.Metric{
				When:  time.Unix(aggStart, 0),
				Value: agg(query.Agg, aggWindow),
			})
			aggStart = when
			aggWindow = make([]int64, 0)
		}
	}

	if len(aggWindow) > 0 {
		result = append(result, instrument.Metric{
			When:  time.Unix(aggStart, 0),
			Value: agg(query.Agg, aggWindow),
		})
	}

	return
}

func processNoAgg(rows *sql.Rows) (metrics []instrument.Metric) {
	for rows.Next() {
		var (
			when  int64
			value int64
		)

		if err := rows.Scan(&when, &value); err != nil {
			log.Fatal(err)
		}

		metrics = append(metrics, instrument.Metric{
			When:  time.Unix(when, 0),
			Value: value,
		})
	}

	return
}

func (s *metricSqlite) Put(req map[string][]instrument.Metric) error {
	var err error

	for key, metrics := range req {
		name := escapeMetricName(key)
		s.declareMetric(name)

		q := fmt.Sprintf(`
			INSERT INTO %s
			(ts, value)
			VALUES
			(?, ?)
		`, name)

		stmt, err := s.db.Prepare(q)

		if err != nil {
			return err
		}

		defer stmt.Close()

		for _, metric := range metrics {
			if _, _err := stmt.Exec(metric.When.Unix(), metric.Value); _err != nil {
				err = errors.Wrap(err, _err.Error())
			}
		}
	}

	return err
}

func agg(name string, vals []int64) int64 {
	switch name {
	case "sum":
		return aggSum(vals)
	case "avg":
		return aggAvg(vals)
	default:
		log.Fatalf("unknown agg function %s", name)
	}

	return 0
}

func aggSum(vals []int64) (total int64) {
	for _, val := range vals {
		total += val
	}

	return
}

func aggAvg(vals []int64) (avg int64) {
	return aggSum(vals) / int64(len(vals))
}

func escapeMetricName(name string) string {
	return strings.Replace(name, ".", "___", -1)
}

func unescapeMetricName(name string) string {
	return strings.Replace(name, "___", ".", -1)
}
