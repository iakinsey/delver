package gateway

import (
	"encoding/json"
	"github.com/iakinsey/delver/types"
)

func defaultDashboard() types.Dashboard {
	score := "features.sentiment.binary_naive_bayes_title"
	filters := []map[string]interface{}{
		{"data_type": "composite", "key": "uri", "title_key": "features.title", "query": map[string]interface{}{}, "options": map[string]bool{"preload": true}},
		{"data_type": "composite", "key": score, "fields": []string{"timestamp", score}, "query": map[string]interface{}{}, "agg": map[string]interface{}{"agg_name": "avg", "agg_field": score, "time_field": "timestamp", "time_window_seconds": 1800}, "options": map[string]bool{"preload": true}},
		{"data_type": "composite", "key": score, "fields": []string{"timestamp", score, "features.country", "request_id"}, "query": map[string]interface{}{}, "options": map[string]bool{"preload": true}},
		{"data_type": "metric", "query": map[string]string{"key": "httpFetcher.message.in"}, "options": map[string]bool{"preload": true}},
	}
	names := []string{"Crawl log", "Title sentiment", "Country sentiment", "Fetch activity"}
	kinds := []string{"crawlLog", "chart", "map", "metric"}
	cells := []interface{}{}
	for i, f := range filters {
		cells = append(cells, map[string]interface{}{"uuid": string(types.NewV4()), "type": kinds[i], "title": names[i], "filter": f, "attributes": map[string]int{"x": (i % 2) * 36, "y": (i / 2) * 5, "w": 36, "h": 5}})
	}
	delete(cells[0].(map[string]interface{}), "filter")
	value, _ := json.Marshal(map[string]interface{}{"cells": cells})
	return types.Dashboard{ID: string(types.NewV4()), Name: "Overview", Description: "Crawl log, title sentiment, country sentiment, and crawler activity.", Value: value}
}
func (s *dashboardGateway) EnsureDefault() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := migrateDefaultCrawlLog(tx); err != nil {
		return err
	}
	result, err := tx.Exec("INSERT OR IGNORE INTO application_metadata(key,value) VALUES ('default_initialized','1')")
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	var count int
	if err = tx.QueryRow("SELECT COUNT(*) FROM dashboard").Scan(&count); err != nil {
		return err
	}
	if n > 0 && count == 0 {
		d := defaultDashboard()
		if _, err = tx.Exec(putDashboard, d.ID, d.Name, d.Value, d.Description); err != nil {
			return err
		}
	}
	return tx.Commit()
}
