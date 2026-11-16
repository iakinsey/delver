package application

import "github.com/iakinsey/delver/config"

// Both input forms converge here before a run is opened or continued.
func prepareRunPipeline(root string, run CrawlRun) (config.Application, error) {
	if run.Expert == nil {
		app, err := CompileCrawl(root, run.ID, run.Definition)
		if err != nil {
			return app, err
		}
		if run.Continued > 0 {
			return continuationPipeline(app)
		}
		return app, nil
	}
	app := *run.Expert
	if !run.Legacy {
		app = relocatePipeline(app, root)
	}
	if run.Continued > 0 {
		return continuationPipeline(app)
	}
	return expertRuntimeControls(app, effectiveTermination(run))
}
