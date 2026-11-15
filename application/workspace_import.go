package application

import (
	"encoding/json"
	"fmt"
	"github.com/iakinsey/delver/config"
	"path/filepath"
	"strings"
)

func sameLegacyPipeline(a, b config.Application) bool {
	a.Config.API.OperationsPath = ""
	b.Config.API.OperationsPath = ""
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
func pipelinePaths(a config.Application) []string {
	out := []string{}
	var visit func(interface{})
	visit = func(v interface{}) {
		switch obj := v.(type) {
		case map[string]interface{}:
			for key, child := range obj {
				if text, ok := child.(string); ok && (key == "path" || key == "base_path" || key == "dlq_path") {
					if abs, err := filepath.Abs(text); err == nil {
						if resolved, e := filepath.EvalSymlinks(abs); e == nil {
							abs = resolved
						}
						out = append(out, filepath.Clean(abs))
					}
				} else {
					visit(child)
				}
			}
		case []interface{}:
			for _, child := range obj {
				visit(child)
			}
		}
	}
	for _, r := range a.Resources {
		var v interface{}
		json.Unmarshal(r.Parameters, &v)
		visit(v)
	}
	for _, worker := range a.Workers {
		var v interface{}
		json.Unmarshal(worker.Parameters, &v)
		visit(v)
	}
	return out
}
func overlapStorage(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}
func (w *Workspace) checkImportOwnership(a config.Application) error {
	for _, r := range w.records {
		owned := []string{filepath.Join(w.root, "crawls", r.ID)}
		for _, run := range r.Runs {
			if run.Legacy && run.Expert != nil {
				owned = append(owned, pipelinePaths(*run.Expert)...)
			}
		}
		for _, wanted := range pipelinePaths(a) {
			for _, path := range owned {
				if overlapStorage(wanted, path) {
					return fmt.Errorf("import shares storage with crawl %q; duplicate that crawl or use separate storage paths", r.Definition.Name)
				}
			}
		}
	}
	return nil
}
