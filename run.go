package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/pcman312/speedtest-exporter/speedtest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vrecan/death"
	"github.com/vrecan/life"
)

func run(log hclog.Logger) (exitCode int) {
	cfg, err := getConfig()
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := http.Server{
		Addr:    ":9801",
		Handler: mux,
	}
	go srv.ListenAndServe()

	r := newRunner(log, cfg.Command, cfg.Servers, cfg.TickRate.Duration)
	r.Start()

	log.Info("speedtest_exporter is running")
	d := death.NewDeath(syscall.SIGINT, syscall.SIGTERM)
	d.WaitForDeath()

	log.Info("Shutting down")

	r.Close()
	srv.Close()
	log.Info("Done shutting down")
	return 0
}

func getConfig() (speedtest.Config, error) {
	rawCfg, err := ioutil.ReadFile(*configFile)
	if err != nil {
		return speedtest.Config{}, fmt.Errorf("unable to read file: %w", err)
	}

	c := speedtest.Config{}
	err = json.Unmarshal(rawCfg, &c)
	if err != nil {
		return speedtest.Config{}, fmt.Errorf("unable to unmarshal config: %w", err)
	}

	return c, nil
}

type runner struct {
	*life.Life
	log    hclog.Logger
	ctx    context.Context
	cancel context.CancelFunc

	cmd       speedtest.Command
	serverIDs []int
	tickRate  time.Duration
}

func newRunner(log hclog.Logger, cmd speedtest.Command, serverIDs []int, tickRate time.Duration) *runner {
	ctx, cancel := context.WithCancel(context.Background())
	r := runner{
		Life:   life.NewLife(),
		log:    log,
		ctx:    ctx,
		cancel: cancel,

		cmd:       cmd,
		serverIDs: serverIDs,
		tickRate:  tickRate,
	}
	r.SetRun(r.run)
	return &r
}

func (r *runner) run() {
	ticker := time.NewTicker(r.tickRate)
	defer ticker.Stop()

	r.runSpeedTests()

	start := time.Now()
	for {
		select {
		case <-r.Life.Done:
			return
		case <-ticker.C:
			wt := time.Now().Sub(start)
			waitTime.Set(wt.Seconds())
			start = time.Now()

			r.log.Info("Running speed tests...")
			r.runSpeedTests()
			r.log.Info("Done running speed tests")
		}
	}
}

const namespace = "speedtest_exporter"

var (
	commonLabels = []string{
		"server_id",
		"server_name",
		"location"}

	runs = promauto.NewCounter(prometheus.CounterOpts{
		Namespace:   namespace,
		Name:        "runs",
		Help:        "Number of times the speed tests have run",
		ConstLabels: nil,
	})
	running = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace:   namespace,
		Name:        "running",
		Help:        "Indicates if the speed test is currently running",
		ConstLabels: nil,
	})
	runTime = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "run_time_seconds",
		Help:      "Amount of time spent running all of the speed tests",
	})
	lastStartTime = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "last_start_time",
		Help:      "Last time the speed test run started",
	})
	lastFinishTime = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "last_finish_time",
		Help:      "Last time the speed test run finished",
	})
	waitTime = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "wait_time_seconds",
		Help:      "Amount of downtime between speed test runs. This does not include time between server test executions",
	})

	downloadSpeed = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "download_bytes_per_second",
		Help:      "Download speed",
	}, commonLabels)
	downloadBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "download_bytes",
		Help:      "Number of bytes downloaded as a part of the test",
	}, commonLabels)
	downloadElapsed = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "download_elapsed_seconds",
		Help:      "How long the download speed test took in seconds",
	}, commonLabels)

	uploadSpeed = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "upload_bytes_per_second",
		Help:      "Upload speed",
	}, commonLabels)
	uploadBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "upload_bytes",
		Help:      "Number of bytes uploaded as a part of the test",
	}, commonLabels)
	uploadElapsed = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "upload_elapsed_seconds",
		Help:      "How long the upload speed test took in seconds",
	}, commonLabels)

	ping = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "ping_seconds",
		Help:      "Ping time for the speed test",
	}, commonLabels)
	jitter = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "ping_jitter_seconds",
		Help:      "Ping jitter for the speed test",
	}, commonLabels)
)

func (r *runner) runSpeedTests() {
	if r.closing() {
		return
	}

	start := time.Now()

	running.Set(1)
	lastStartTime.Set(float64(start.Unix()))
	defer func() {
		lastFinishTime.Set(float64(time.Now().Unix()))
		running.Set(0)
	}()

	results, err := speedtest.RunTests(r.ctx, r.log, r.cmd, r.serverIDs...)
	if err != nil {
		r.log.Error("Speed tests failed",
			"err", err,
			"requested", len(r.serverIDs),
			"succeeded", len(results),
		)
	} else {
		r.log.Info("Speed tests succeeded", "num_tests", len(results))
	}
	runs.Add(1)

	for _, result := range results {
		labels := []string{
			strconv.Itoa(result.Server.ID),
			result.Server.Name,
			result.Server.Location,
		}

		downloadSpeed.WithLabelValues(labels...).Set(result.Download.Bandwidth)
		downloadBytes.WithLabelValues(labels...).Set(result.Download.Bytes)
		downloadElapsed.WithLabelValues(labels...).Set(result.Download.Elapsed)

		uploadSpeed.WithLabelValues(labels...).Set(result.Upload.Bandwidth)
		uploadBytes.WithLabelValues(labels...).Set(result.Upload.Bytes)
		uploadElapsed.WithLabelValues(labels...).Set(result.Upload.Elapsed)

		rawPing := time.Duration(result.Ping.Latency) * time.Millisecond
		rawJitter := time.Duration(result.Ping.Jitter) * time.Millisecond
		ping.WithLabelValues(labels...).Set(rawPing.Seconds())
		jitter.WithLabelValues(labels...).Set(rawJitter.Seconds())
	}

	dur := time.Now().Sub(start)
	runTime.Set(dur.Seconds())
}

func (r *runner) Close() error {
	r.cancel()
	return r.Life.Close()
}

func (r *runner) closing() bool {
	select {
	case <-r.ctx.Done():
		r.log.Info("IsClosing", "closing", true)
		return true
	default:
		r.log.Info("IsClosing", "closing", false)
		return false
	}
}
