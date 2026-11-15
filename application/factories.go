package application

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/armon/go-metrics"
	"reflect"

	"github.com/pkg/errors"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/instrument"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/resource/bloom"
	"github.com/iakinsey/delver/resource/maps"
	"github.com/iakinsey/delver/resource/objectstore"
	"github.com/iakinsey/delver/worker"
	"github.com/iakinsey/delver/worker/accumulator"
	"github.com/iakinsey/delver/worker/extractor"
	"github.com/iakinsey/delver/worker/fetcher"
	"github.com/iakinsey/delver/worker/publisher"
	"github.com/iakinsey/delver/worker/transformer"
)

var resourceKeyLookupError = errors.New("failed to find resource key")

type preparedApplication struct {
	metrics   metrics.MetricSink
	ctx       context.Context
	app       config.Application
	resources map[string]interface{}
	workers   map[string]worker.WorkerManager
}

func CreateWorker(wc config.Worker, preparedApp *preparedApplication) {
	var w worker.Worker

	switch wc.Type {
	case "jsonl_sink":
		params := worker.JSONLParams{}
		parseParam(wc.Parameters, &params)
		var err error
		w, err = worker.NewJSONLWorker(params)
		if err != nil {
			panic(err)
		}
	case "dfs_basic_accumulator":
		dbap := accumulator.DfsBasicAccumulatorParams{}
		parseParamWithResources(wc.Parameters, &dbap, preparedApp.resources)
		w = accumulator.NewDfsBasicAccumulator(dbap)
	case "news_accumulator":
		nap := accumulator.NewsAccumulatorParams{}
		parseParamWithResources(wc.Parameters, &nap, preparedApp.resources)
		w = accumulator.NewNewsAccumulator(nap)
	case "composite_extractor":
		cap := extractor.CompositeArgs{}
		parseParamWithResources(wc.Parameters, &cap, preparedApp.resources)
		w = extractor.NewCompositeExtractorWorkerWithConfig(cap, preparedApp.app.Config)
	case "http_fetcher":
		hfp := fetcher.HttpFetcherParams{}
		parseParamWithResources(wc.Parameters, &hfp, preparedApp.resources)
		w = fetcher.NewHttpFetcherWithConfig(hfp, preparedApp.app.Config)
	case "dfs_basic_publisher":
		dbp := publisher.DfsBasicPublisherParams{}
		parseParamWithResources(wc.Parameters, &dbp, preparedApp.resources)
		w = publisher.NewDfsBasicPublisherWithConfig(dbp, preparedApp.app.Config)
	case "rss_feed_publisher":
		rfp := publisher.RssFeedPublisherParams{}
		parseParamWithResources(wc.Parameters, &rfp, preparedApp.resources)
		w = publisher.NewRssFeedPublisherWithConfig(rfp, preparedApp.app.Config)
	case "fixed_seed_publisher":
		fsp := publisher.FixedSeedPublisherParams{}
		parseParamWithResources(wc.Parameters, &fsp, preparedApp.resources)
		w = publisher.NewFixedSeedPublisher(fsp)
	case "transformer":
		tfp := transformer.TransformerParams{}
		parseParamWithResources(wc.Parameters, &tfp, preparedApp.resources)
		w = transformer.NewTransformerWorkerContext(preparedApp.ctx, tfp)
	default:
		registryMutex.RLock()
		factory := workerFactories[wc.Type]
		contextual := runtimeWorkerFactories[wc.Type]
		registryMutex.RUnlock()
		if factory == nil {
			panic("unknown worker type " + wc.Type)
		}
		var err error
		if contextual != nil {
			w, err = contextual(preparedApp.ctx, wc.Parameters, preparedApp.resources, preparedApp.dependencies())
		} else {
			w, err = factory(wc.Parameters, preparedApp.resources)
		}
		if err != nil {
			panic(err)
		}
	}

	if wc.Once {
		store, ok := preparedApp.resources["source_progress"].(maps.Map)
		if !ok {
			panic("once source requires source_progress map")
		}
		w = &worker.OnceWorker{Worker: w, Store: store, Key: wc.Name}
	}
	if preparedApp.metrics != nil {
		if target, ok := w.(interface{ SetMetricSink(metrics.MetricSink) }); ok {
			target.SetMetricSink(preparedApp.metrics)
		}
	}
	m := GetWorkerManager(wc, preparedApp.resources, w)
	if preparedApp.metrics != nil {
		m.(interface{ SetMetricSink(metrics.MetricSink) }).SetMetricSink(preparedApp.metrics)
	}
	preparedApp.workers[wc.Name] = m
}

func GetWorkerManager(wc config.Worker, resources map[string]interface{}, w worker.Worker) (m worker.WorkerManager) {
	inbox, ok := resources[wc.Inbox]

	if !ok && wc.Manager != "job" {
		panic(fmt.Errorf("worker %s has no inbox %s", wc.Name, wc.Inbox))
	}

	out, ok := resources[wc.Outbox]
	var outbox queue.Queue = nil

	if ok {
		outbox = out.(queue.Queue)
	}

	switch wc.Manager {
	case "worker", "":
		m = worker.NewWorkerManager(
			w,
			inbox.(queue.Queue),
			outbox,
		)
	case "job":
		m = worker.NewJobManager(
			w,
			outbox,
			wc.Interval,
		)
	default:
		panic(fmt.Errorf("unknown worker manager: %s", wc.Manager))
	}

	return
}

func CreateResource(c config.Resource, preparedApp *preparedApplication) {
	var r interface{}

	switch c.Type {
	case "crawl_queue":
		params := queue.CrawlQueueParams{MaxAttempts: 3}
		parseParam(c.Parameters, &params)
		var err error
		r, err = queue.NewCrawlQueue(params)
		if err != nil {
			panic(err)
		}
	case "file_queue":
		fqp := queue.FileQueueParams{Resilient: true, Reset: true, MaxAttempts: 3}
		parseParam(c.Parameters, &fqp)
		r = queue.NewFileQueue(fqp)
	case "channel_queue":
		r = queue.NewChannelQueue()
	case "timer":
		tp := queue.TimerQueueParams{}
		parseParam(c.Parameters, &tp)
		r = queue.NewTimerQueue(tp)
	case "bloom_filter":
		bfp := bloom.BloomFilterParams{}
		parseParam(c.Parameters, &bfp)
		r = bloom.NewBloomFilter(bfp)
	case "rolling_bloom_filter":
		rbfp := bloom.RollingBloomFilterParams{SaveInterval: preparedApp.app.Config.DefaultSaveInterval}
		parseParam(c.Parameters, &rbfp)
		r = bloom.NewRollingBloomFilter(rbfp)
	case "persistent_map":
		pmp := maps.PersistentMapParams{}
		parseParam(c.Parameters, &pmp)
		r = maps.NewPersistentMapWithConfig(pmp, preparedApp.app.Config.PersistentMap)
	case "multi_host_map":
		mhmp := maps.MultiHostMapParams{}
		parseParam(c.Parameters, &mhmp)
		r = maps.NewMultiHostMapWithConfig(mhmp, preparedApp.app.Config.PersistentMap)
	case "filesystem_object_store":
		fosp := objectstore.FilesystemObjectStoreParams{}
		parseParam(c.Parameters, &fosp)
		r = objectstore.NewFilesystemObjectStore(fosp)
	default:
		registryMutex.RLock()
		factory := resourceFactories[c.Type]
		contextual := runtimeResourceFactories[c.Type]
		registryMutex.RUnlock()
		if factory == nil {
			panic("unknown resource " + c.Type)
		}
		var err error
		if contextual != nil {
			r, err = contextual(preparedApp.ctx, c.Parameters, preparedApp.dependencies())
		} else {
			r, err = factory(c.Parameters)
		}
		if err != nil {
			panic(err)
		}
	}

	// Set metrics value if resoruce is specified
	if c.Name == config.TransformerQueueName && preparedApp.ctx.Value(runtimeKey{}) == nil {
		instrument.SetMetrics(r.(queue.Queue))
	}

	preparedApp.resources[c.Name] = r
}

func parseParam(data []byte, config interface{}) {
	if err := json.Unmarshal(data, config); err != nil {
		panic(err)
	}
}

func parseParamWithResources(data []byte, config interface{}, resources map[string]interface{}) {
	parseParam(data, config)

	rType := reflect.TypeOf(config)
	rValue := reflect.ValueOf(config)
	valelem := rType.Elem()

	for i := 0; i < valelem.NumField(); i++ {
		field := valelem.Field(i)

		if resourceTag, ok := field.Tag.Lookup("resource"); ok && resourceTag != "" {
			resource := getResource(data, config, resourceTag, resources)
			if err := assignResource(rValue.Elem().Field(i), resource); err != nil {
				panic(err)
			}
		}
	}
}

func getResource(data []byte, c interface{}, resourceKey string, resources map[string]interface{}) interface{} {
	resourceName, err := getResourceName(data, resourceKey)

	if errors.Is(err, resourceKeyLookupError) && (resourceKey == config.TransformerQueueName || resourceKey == "visited_urls" || resourceKey == "seen_urls") {
		resourceName = resourceKey
	} else if errors.Is(err, resourceKeyLookupError) {
		panic(fmt.Errorf("missing resource %s", resourceKey))
	} else if err != nil {
		panic(err)
	}

	resource, ok := resources[resourceName]

	if !ok && (resourceKey == config.TransformerQueueName || resourceKey == "visited_urls" || resourceKey == "seen_urls") {
		return nil
	}
	if !ok {
		panic("resource " + resourceName + " not defined")
	}

	return resource
}

func getResourceName(data []byte, key string) (string, error) {
	m := make(map[string]json.RawMessage)

	if err := json.Unmarshal(data, &m); err != nil {
		return "", errors.Wrapf(err, "failed to parse resource json %s", key)
	}

	r, ok := m[key]

	if !ok {
		return "", resourceKeyLookupError
	}

	var res string

	if err := json.Unmarshal(r, &res); err != nil {
		return "", errors.Wrapf(err, "failed to parse resource value %s", key)
	}

	return res, nil
}
