package main

import (
	"encoding/json"
	"io"
	"os"
	"os/signal"
	"reflect"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/api"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/gateway"
	"github.com/iakinsey/delver/instrument"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/resource/bloom"
	"github.com/iakinsey/delver/resource/logger"
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

const terminationWaitTime = 2 * time.Second

type preparedApplication struct {
	app       config.Application
	resources map[string]interface{}
	workers   map[string]worker.WorkerManager
}

func main() {
	if len(os.Args) <= 1 {
		log.Fatalf("Config path must be provided")
	}

	StartFromJsonConfig(os.Args[1])
}

func StartFromJsonConfig(path string) {
	app := config.Application{}
	inter := config.RawApplication{}
	f, err := os.Open(path)

	if err != nil {
		log.Fatalf("failed to open config: %s", path)
	}

	b, err := io.ReadAll(f)

	if err != nil {
		log.Fatalf("failed to read config: %s", path)
	}

	if err := json.Unmarshal(b, &inter); err != nil {
		log.Fatalf("failed to parse intermediate config: %s", path)
	}

	config.Set(inter.Config)

	if err := json.Unmarshal(b, &app); err != nil {
		log.Fatalf("falsed to parse config: %s", path)
	}

	// Set config after parsing so defaults are set
	app.Config = config.Get()

	go api.StartHTTPServer()
	go gateway.StartClientStreamer()
	StartFromApplication(app)
}

func StartFromApplication(app config.Application) {
	preparedApp := &preparedApplication{
		app:       app,
		resources: make(map[string]interface{}),
		workers:   make(map[string]worker.WorkerManager),
	}

	// Put the transformer queue at the start of the list, allowing
	// other resources to access it in a stable manner
	sort.SliceStable(app.Resources, func(i, j int) bool {
		return app.Resources[i].Name == config.TransformerQueueName
	})

	for _, rc := range app.Resources {
		CreateResource(rc, preparedApp)
	}

	for _, wc := range app.Workers {
		CreateWorker(wc, preparedApp)
	}

	StartApplication(app, preparedApp.resources, preparedApp.workers)
	AwaitTermination(preparedApp.resources, preparedApp.workers)
}

func StartApplication(app config.Application, resources map[string]interface{}, workers map[string]worker.WorkerManager) {
	conf := app.Config.Workers

	for _, resource := range resources {
		if q, ok := resource.(queue.Queue); ok {
			go q.Start()
		}
	}

	if !conf.Enabled {
		return
	}

	for _, wc := range app.Workers {
		manager := workers[wc.Name]
		count := wc.Count

		// Defaults to 0
		if count == 0 && wc.Manager == "job" {
			count = 1
		} else if count == 0 {
			count = conf.WorkerCounts
		}

		for i := 0; i < count; i++ {
			go manager.Start()
		}
	}
}

func AwaitTermination(resources map[string]interface{}, workers map[string]worker.WorkerManager) {
	done := make(chan bool)
	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	<-sigterm

	go Terminate(resources, workers, done)

	log.Info("terminating")

	select {
	case <-time.After(terminationWaitTime):
		log.Info("termination grace period expired, exiting dirty")
		os.Exit(1)
	case <-done:
		log.Info("terminated successfully")
		os.Exit(0)
	}
}

func Terminate(resources map[string]interface{}, workers map[string]worker.WorkerManager, done chan bool) {
	var wg sync.WaitGroup

	for _, resource := range resources {
		if q, ok := resource.(queue.Queue); ok {
			wg.Add(1)

			go func() {
				defer wg.Done()
				q.Stop()
			}()
		}
	}

	for _, m := range workers {
		wg.Add(1)

		go func(m worker.WorkerManager) {
			defer wg.Done()
			m.Stop()
		}(m)
	}

	wg.Wait()

	done <- true
}

func CreateWorker(wc config.Worker, preparedApp *preparedApplication) {
	var w worker.Worker

	switch wc.Type {
	case "dfs_basic_accumulator":
		dbap := accumulator.DfsBasicAccumulatorParams{}
		parseParamWithResources(wc.Parameters, &dbap, preparedApp.resources)
		w = accumulator.NewDfsBasicAccumulator(dbap)
	case "news_accumulator":
		nap := accumulator.NewsAccumulatorParams{}
		parseParamWithResources(wc.Parameters, &nap, preparedApp.resources)
		w = accumulator.NewNewsAccumulator(nap)
	case "resource_accumulator":
		rap := accumulator.ResourceAccumulatorParams{}
		parseParamWithResources(wc.Parameters, &rap, preparedApp.resources)
		w = accumulator.NewResourceAccumulator(rap)
	case "composite_extractor":
		cap := extractor.CompositeArgs{}
		parseParamWithResources(wc.Parameters, &cap, preparedApp.resources)
		w = extractor.NewCompositeExtractorWorker(cap)
	case "http_fetcher":
		hfp := fetcher.HttpFetcherParams{}
		parseParamWithResources(wc.Parameters, &hfp, preparedApp.resources)
		w = fetcher.NewHttpFetcher(hfp)
	case "dfs_basic_publisher":
		dbp := publisher.DfsBasicPublisherParams{}
		parseParamWithResources(wc.Parameters, &dbp, preparedApp.resources)
		w = publisher.NewDfsBasicPublisher(dbp)
	case "rss_feed_publisher":
		rfp := publisher.RssFeedPublisherParams{}
		parseParamWithResources(wc.Parameters, &rfp, preparedApp.resources)
		w = publisher.NewRssFeedPublisher(rfp)
	case "fixed_seed_publisher":
		fsp := publisher.FixedSeedPublisherParams{}
		parseParamWithResources(wc.Parameters, &fsp, preparedApp.resources)
		w = publisher.NewFixedSeedPublisher(fsp)
	case "transformer":
		tfp := transformer.TransformerParams{}
		parseParamWithResources(wc.Parameters, &tfp, preparedApp.resources)
		w = transformer.NewTransformerWorker(tfp)
	default:
		log.Fatalf("unknown worker type %s", wc.Type)
	}

	preparedApp.workers[wc.Name] = GetWorkerManager(wc, preparedApp.resources, w)
}

func GetWorkerManager(wc config.Worker, resources map[string]interface{}, w worker.Worker) (m worker.WorkerManager) {
	inbox, ok := resources[wc.Inbox]

	if !ok && wc.Manager != "job" {
		log.Fatalf("worker %s has no inbox %s", wc.Name, wc.Inbox)
	}

	out, ok := resources[wc.Outbox]
	var outbox queue.Queue = nil

	if ok {
		outbox = out.(queue.Queue)
	}

	// TODO some workers require outboxes, how can this be transparently handled at init time?
	/*
		if !ok {
			log.Fatalf("worker %s has no outbox %s", wc.Name, wc.Outbox)
		}
	*/

	switch wc.Manager {
	case "worker":
	case "":
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
		log.Fatalf("unknown worker manager: %s", wc.Manager)
	}

	return
}

func CreateResource(c config.Resource, preparedApp *preparedApplication) {
	var r interface{}

	switch c.Type {
	case "file_queue":
		fqp := queue.FileQueueParams{Resilient: true}
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
		rbfp := bloom.RollingBloomFilterParams{}
		parseParam(c.Parameters, &rbfp)
		r = bloom.NewRollingBloomFilter(rbfp)
	case "persistent_map":
		pmp := maps.PersistentMapParams{}
		parseParam(c.Parameters, &pmp)
		r = maps.NewPersistentMap(pmp)
	case "multi_host_map":
		mhmp := maps.MultiHostMapParams{}
		parseParam(c.Parameters, &mhmp)
		r = maps.NewMultiHostMap(mhmp)
	case "filesystem_object_store":
		fosp := objectstore.FilesystemObjectStoreParams{}
		parseParam(c.Parameters, &fosp)
		r = objectstore.NewFilesystemObjectStore(fosp)
	case "hdfs_logger":
		hlp := logger.HDFSLoggerParams{}
		parseParam(c.Parameters, &hlp)
		r = logger.NewHDFSLogger(hlp)
	case "elasticsearch_logger":
		elp := logger.ElasticsearchLoggerParams{}
		parseParam(c.Parameters, &elp)
		r = logger.NewElasticsearchLogger(elp)
	default:
		log.Fatalf("unknown resource %s", c.Type)
	}

	// Set metrics value if resoruce is specified
	if c.Name == config.TransformerQueueName {
		instrument.SetMetrics(r.(queue.Queue))
	}

	preparedApp.resources[c.Name] = r
}

func parseParam(data []byte, config interface{}) {
	if err := json.Unmarshal(data, config); err != nil {
		log.Fatalf("failed to parse queue object ")
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
			f := reflect.New(reflect.TypeOf(resource))

			f.Elem().Set(reflect.ValueOf(resource))
			rValue.Elem().Field(i).Set(f.Elem())
		}
	}
}

func getResource(data []byte, c interface{}, resourceKey string, resources map[string]interface{}) interface{} {
	resourceName, err := getResourceName(data, resourceKey)

	if errors.Is(err, resourceKeyLookupError) && resourceKey == config.TransformerQueueName {
		resourceName = config.TransformerQueueName
	} else if errors.Is(err, resourceKeyLookupError) {
		log.Fatalf("%s: %s", resourceKeyLookupError.Error(), resourceKey)
	} else if err != nil {
		log.Fatalf(err.Error())
	}

	resource, ok := resources[resourceName]

	if !ok {
		log.Fatalf("resource %s not defined", resourceName)
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
