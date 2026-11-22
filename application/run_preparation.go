package application

import (
	"encoding/json"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/util"
	"strings"
)

// Both input forms converge here before a run is opened or continued.
func prepareRunPipeline(root string, run CrawlRun) (config.Application, error) {
	if run.Expert == nil {
		app, err := compileCrawl(root, run.ID, run.Definition, true)
		if err != nil {
			return app, err
		}
		if run.Continued > 0 {
			return continuationPipeline(app)
		}
		return app, nil
	}
	app, err := prepareApplication(*run.Expert)
	if err != nil {
		return app, err
	}
	if !run.Legacy {
		app = relocatePipeline(app, root)
	}
	if run.Continued > 0 {
		return continuationPipeline(app)
	}
	return expertRuntimeControls(app, effectiveTermination(run))
}

// prepareApplication is shared by decoded custom pipelines and guided compilation.
// Preserve opaque resource parameters as RawMessage, including large integers.
// Global policy is inherited by factories at execution, not copied into saved
// resource parameters, so removing a global rule cannot leave a stale local copy.
func prepareApplication(app config.Application) (config.Application, error) {
	extensions, err := util.NormalizeExtensions(strings.Join(app.Config.HTTPClient.ExcludedExtensions, ","))
	if err != nil {
		return app, err
	}
	app.Config.HTTPClient.ExcludedExtensions = extensions
	app.Resources = append([]config.Resource(nil), app.Resources...)
	for i, r := range app.Resources {
		if r.Type != "frontier_store" {
			continue
		}
		var fields map[string]json.RawMessage
		if len(r.Parameters) > 0 {
			if err := json.Unmarshal(r.Parameters, &fields); err != nil {
				return app, err
			}
		}
		if fields == nil {
			fields = map[string]json.RawMessage{}
		}
		var local []string
		if raw, ok := fields["excluded_extensions"]; ok {
			if err := json.Unmarshal(raw, &local); err != nil {
				return app, err
			}
		}
		all, err := util.NormalizeExtensions(strings.Join(local, ","))
		if err != nil {
			return app, err
		}
		if len(all) > 0 {
			fields["excluded_extensions"], _ = json.Marshal(all)
			app.Resources[i].Parameters, _ = json.Marshal(fields)
		}
	}
	return app, Validate(app)
}

// Workspace previews and saves apply the same protection bounds as startup.
func (w *Workspace) compileCrawl(root, runID string, d CrawlDefinition) (config.Application, error) {
	app, e := CompileCrawl(root, runID, d)
	if e != nil {
		return app, e
	}
	app.Config.HTTPClient.Protection = w.protection
	return app, Validate(app)
}
