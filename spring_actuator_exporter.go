package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

const (
	namespace = "spring_actuator"
)

type Exporter struct {
	URL           string
	up            prometheus.Gauge
	springMetrics map[string]*prometheus.GaugeVec
	client        *http.Client
}

func NewExporter(url string, timeout time.Duration) *Exporter {
	return &Exporter{
		URL: url,
		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "up",
			Help:      "Was the last scrape of Spring Actuator successful",
		}),
		springMetrics: map[string]*prometheus.GaugeVec{
			"mem":                   newMetrics("mem", "The total system memory in KB", nil, []string{"memory"}),
			"mem.free":              newMetrics("mem_free", "The amount of free memory in KB", nil, []string{"memory"}),
			"heap.committed":        newMetrics("heap_committed", "Heap information in KB", nil, []string{"memory"}),
			"heap.used":             newMetrics("heap_used", "Heap information in KB", nil, []string{"memory"}),
			"nonheap.committed":     newMetrics("nonheap_committed", "Non heap information in KB", nil, []string{"memory"}),
			"nonheap.used":          newMetrics("nonheap_used", "Non heap information in KB", nil, []string{"memory"}),
			"threads":               newMetrics("threads", "Thread information", nil, []string{"thread"}),
			"classes":               newMetrics("classes", "Class load information", nil, []string{"classes"}),
			"classes.loaded":        newMetrics("classes_loaded", "Class load information", nil, []string{"classes"}),
			"classes.unloaded":      newMetrics("classes_unloaded", "Class load information", nil, []string{"classes"}),
			"gc.ps_scavenge.count":  newMetrics("gc_ps_scavenge_count", "Garbage collection information", nil, []string{"gc"}),
			"gc.ps_scavenge.time":   newMetrics("gc_ps_scavenge_time", "Garbage collection information", nil, []string{"gc"}),
			"gc.ps_marksweep.count": newMetrics("gc_ps_marksweep_count", "Garbage collection information", nil, []string{"gc"}),
			"gc.ps_marksweep.time":  newMetrics("gc_ps_marksweep_time", "Garbage collection information", nil, []string{"gc"}),
			"systemload.average":    newMetrics("systemload_average", "The average system load", nil, []string{"load_average"}),
		},
		client: &http.Client{
			Transport: &http.Transport{
				Dial: func(netw, addr string) (net.Conn, error) {
					c, err := net.DialTimeout(netw, addr, timeout)
					if err != nil {
						return nil, err
					}
					if err := c.SetDeadline(time.Now().Add(timeout)); err != nil {
						return nil, err
					}
					return c, nil
				},
			},
		},
	}
}

func (e *Exporter) scrape() {
	resp, err := e.client.Get(e.URL)
	if err != nil {
		e.up.Set(0)
		log.Errorf("Can't scrape Spring Actuator: %v", err)
		return
	}
	defer resp.Body.Close()

	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		e.up.Set(0)
		log.Errorf("Can't scrape Spring Actuator: StatusCode: %d", resp.StatusCode)
		return
	}
	e.up.Set(1)
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Reading response body failed %v", err)
		return
	}

	var metrics map[string]*json.RawMessage
	if err := json.Unmarshal(body, &metrics); err != nil {
		log.Fatalf("JSON unmarshaling failed: %s", err)
	}
	e.export(metrics)
}

func (e *Exporter) export(metrics map[string]*json.RawMessage) {
	for k, v := range metrics {
		_, ok := e.springMetrics[k]
		if !ok {
			continue
		}
		switch k {
		case "systemload.average":
			var tmp float64
			json.Unmarshal(*v, &tmp)
			e.springMetrics[k].WithLabelValues(k).Set(tmp)
		default:
			var tmp uint64
			json.Unmarshal(*v, &tmp)
			e.springMetrics[k].WithLabelValues(k).Set(float64(tmp))
		}
	}
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.up.Desc()
	for _, m := range e.springMetrics {
		m.Describe(ch)
	}
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.resetMetrics()
	e.scrape()
	ch <- e.up
	for _, m := range e.springMetrics {
		m.Collect(ch)
	}
}

func (e *Exporter) resetMetrics() {
	for _, m := range e.springMetrics {
		m.Reset()
	}
}

func newMetrics(name string, help string, constLabels prometheus.Labels, labels []string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        name,
			Help:        help,
			ConstLabels: constLabels,
		},
		labels,
	)
}

func main() {
	var (
		listenAddress     = flag.String("web.listen-address", ":9101", "Address to listen on for web interface and telemetry.")
		metricsPath       = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		actuatorScrapeURI = flag.String("actuator.scrape-uri", "http://localhost/metrics", "URI on which to scrape Spring Actuator.")
		timeout           = flag.Duration("actuator.timeout", 5*time.Second, "Timeout for trying to get stats from Spring Actuator.")
	)
	flag.Parse()
	exporter := NewExporter(*actuatorScrapeURI, *timeout)
	prometheus.MustRegister(exporter)
	log.Infof("Starting Server: %s", *listenAddress)
	http.Handle(*metricsPath, prometheus.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
		<head><title>Spring Actuator Exporter</title></head>
		<body>
		<h1>Spring Actuator Exporter</h1>
		<p><a href='` + *metricsPath + `'>Metrics</a></p>
		</body>
		</html>`))
	})
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
