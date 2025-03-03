package main

import (
	"flag"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mercury200Hg/metrics-server-prometheus-exporter/exporter"
	"github.com/mercury200Hg/metrics-server-prometheus-exporter/utils"

	"github.com/rs/zerolog"

	"github.com/rs/zerolog/log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Job definition
type Job struct {
	Type  string
	Sleep time.Duration
}

var (
	types       = []string{"pods", "nodes"}
	workers     = 2
	iterations  = 0
	interval, _ = time.ParseDuration("30s")

	inflightCounterVec = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "worker",
			Subsystem: "jobs",
			Name:      "metrics_server_exporter_inflight_jobs",
			Help:      "Number of jobs in flight for metrics-server-exporter go routine",
		},
		[]string{"type"},
	)
)

func initFlags() {
	flag.IntVar(&workers, "workers", workers, "Number of workers to use")
	flag.DurationVar(&interval, "interval", interval, "Duration at which to collect data from the metrics server api")
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "<h1>Metrics-Server-Exporter</h1><br><div>Please visit <a href='/metrics'>/metrics</a> to see metrics </div>")
}

func logRequestHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL)
		handler.ServeHTTP(w, r)
	})
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.ParseLevel("info")
	initFlags()
	flag.Parse()

	log.Info().Msg("Checking kube-api. Searching for config file/service-accounts...")
	status := utils.CheckKubeAPI()

	if status == false {
		log.Error().Msg("Unable to verify kube config")
	} else {
		log.Info().Msg("Kube config verified successfully.")
	}

	prometheus.MustRegister(
		exporter.PodMetricCPU,
		exporter.PodMetricMemory,
		exporter.NodeMetricCPU,
		exporter.NodeMetricMemory,
	)

	// create a channel with a 100 Job buffer
	jobsChannel := make(chan *Job, 100)

	go startJobProcessor(jobsChannel)
	go createJobs(jobsChannel)
	log.Info().Msgf("Starting application on port: 9100")
	handler := http.NewServeMux()
	handler.HandleFunc("/", rootHandler)
	handler.Handle("/metrics", logRequestHandler(promhttp.Handler()))
	log.Fatal().Err(http.ListenAndServe(fmt.Sprintf(":9100"), handler))
}

// makeJob creates a new job in channel at rate of given sleep time
func makeJob(jobType string) *Job {
	duration, _ := time.ParseDuration("30s")
	return &Job{
		Type:  jobType,
		Sleep: duration,
	}
}

func startJobProcessor(jobs <-chan *Job) {
	log.Info().Msgf("Starting %d workers", workers)
	wait := sync.WaitGroup{}
	wait.Add(workers)

	// start given workers
	for i := 0; i < workers; i++ {
		go func(workerID int) {
			// start the worker
			startWorker(workerID, jobs)
			wait.Done()
		}(i)
	}
	wait.Wait()
}

func createJobs(jobs chan<- *Job) {
	for {
		// create jobs
		for i := 0; i < len(types); i++ {
			job := makeJob(types[i])
			if i%2 == 0 {
				inflightCounterVec.WithLabelValues(job.Type).Inc()
			}
			jobs <- job
		}
		// don't file up queue too quickly
		generationTime, _ := time.ParseDuration("30s")
		time.Sleep(generationTime)
	}
}

// creates a worker that pulls job from job channel
func startWorker(workerID int, jobs <-chan *Job) {
	for {
		select {
		// read from the job channel
		case job := <-jobs:
			startTime := time.Now()
			if job.Type == "nodes" {
				exporter.RecordNodeMetrics()
				log.Info().Msgf("Scrape count:[%d], Worker:[%d]. Processed job for [%s] in %0.3f seconds", iterations, workerID, job.Type, time.Now().Sub(startTime).Seconds())
			} else if job.Type == "pods" {
				exporter.RecordPodMetrics()
				log.Info().Msgf("Scrape count:[%d], Worker:[%d]. Processed job for [%s] in %0.3f seconds", iterations, workerID, job.Type, time.Now().Sub(startTime).Seconds())
				// Increase the iteration count
				iterations++
			}

			// Sleep to prevent excess load
			log.Info().Msgf("Sleeping workers for %s seconds", job.Sleep.String())
			time.Sleep(job.Sleep)
		}
	}
}
