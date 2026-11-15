package application

func stripObsoleteAccountConfig(value interface{}) {
	root, ok := value.(map[string]interface{})
	if !ok {
		return
	}
	if settings, ok := root["config"].(map[string]interface{}); ok {
		if api, ok := settings["api"].(map[string]interface{}); ok {
			delete(api, "admin_emails")
			delete(api, "user_db_path")
		}
	}
	if operations, ok := root["operations"].(map[string]interface{}); ok {
		for _, operation := range operations {
			if record, ok := operation.(map[string]interface{}); ok {
				delete(record, "user_id")
			}
		}
	}
	for _, key := range []string{"active", "draft", "previous"} {
		if child, ok := root[key]; ok {
			stripObsoleteAccountConfig(child)
		}
	}
}
