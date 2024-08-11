package metrics

import (
	"strings"
	"sync"
	"time"

	"github.com/coocood/freecache"
	dto "github.com/prometheus/client_model/go"
	io_prometheus_client "github.com/prometheus/client_model/go"
	klog "k8s.io/klog/v2"

	"ext-proc/cache"
)

func NewStore(fetcher Fetcher, modelCache, requestCache *freecache.Cache, pods []cache.Pod) *Store {
	return &Store{
		fetcher:                           fetcher,
		cacheActiveLoraModel:              modelCache,
		cachePendingRequestActiveAdapters: requestCache,
		pods:                              pods,
	}
}

type Fetcher interface {
	Fetch(pod cache.Pod) (map[string]*dto.MetricFamily, error)
}

type Store struct {
	fetcher                           Fetcher
	cacheActiveLoraModel              *freecache.Cache
	cachePendingRequestActiveAdapters *freecache.Cache
	// TODO: refresh pod dynamically
	pods []cache.Pod
}

// Contains checks if a slice contains a specific element
func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (s *Store) refreshLoraMetrics(pod cache.Pod, metricFamilies map[string]*dto.MetricFamily) {
	var adapterList []string
	modelsDict := make(map[string]int)

	for name, mf := range metricFamilies {
		if name == "vllm:active_lora_adapters" {
			for _, m := range mf.GetMetric() {
				modelName := getLabelValue(m, "active_lora_adapters")
				numberOfPendingRequests := int(m.GetGauge().GetValue())
				modelsDict[modelName] = numberOfPendingRequests
			}
		}
		if name == "vllm:info_active_adapters_info" {
			for _, metric := range mf.GetMetric() {
				for _, label := range metric.GetLabel() {
					if label.GetName() == "active_adapters" {
						if label.GetValue() != "" {
							adapterList = strings.Split(label.GetValue(), ",")
						}
					}
				}
			}
		}
	}

	for modelName, numberOfPendingRequests := range modelsDict {
		if !Contains(adapterList, modelName) {
			continue
		}
		loraMetric := cache.ActiveLoraModelMetrics{
			Date:                    time.Now().Format(time.RFC3339),
			Pod:                     pod,
			ModelName:               modelName,
			NumberOfPendingRequests: numberOfPendingRequests,
		}
		if err := cache.SetCacheActiveLoraModel(s.cacheActiveLoraModel, loraMetric); err != nil {
			klog.Errorf("Error setting cache: %v", err)
		}
	}
	return
}

func (s *Store) refreshRequestMetrics(pod cache.Pod, metricFamilies map[string]*dto.MetricFamily) {
	pendingRequests := 0
	adapterCount := 0

	for name, mf := range metricFamilies {
		switch name {
		case "vllm:num_requests_waiting":
			for _, m := range mf.GetMetric() {
				pendingRequests += int(m.GetGauge().GetValue())
			}
		case "vllm:num_requests_running":
			for _, m := range mf.GetMetric() {
				pendingRequests += int(m.GetGauge().GetValue())
			}
		case "vllm:info_active_adapters_info":
			for _, metric := range mf.GetMetric() {
				for _, label := range metric.GetLabel() {
					if label.GetName() == "active_adapters" {
						if label.GetValue() != "" {
							adapterCount = len(strings.Split(label.GetValue(), ","))
						}
					}
				}
			}
		}
	}

	requestMetric := cache.PendingRequestActiveAdaptersMetrics{
		Date:                   time.Now().Format(time.RFC3339),
		Pod:                    pod,
		PendingRequests:        pendingRequests,
		NumberOfActiveAdapters: adapterCount,
	}
	if err := cache.SetCachePendingRequestActiveAdapters(s.cachePendingRequestActiveAdapters, requestMetric); err != nil {
		klog.Errorf("Error setting cache: %v", err)
	}
	return
}

// getLabelValue returns the value of a label from a Prometheus metric
func getLabelValue(m *io_prometheus_client.Metric, label string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == label {
			return l.GetValue()
		}
	}
	return ""
}

// FetchMetricsPeriodically fetches metrics periodically and updates the cache
func (s *Store) FetchMetricsPeriodically(interval time.Duration) {
	for {
		s.cacheActiveLoraModel.Clear()
		s.cachePendingRequestActiveAdapters.Clear()
		var wg sync.WaitGroup
		for _, pod := range s.pods {
			pod := pod
			wg.Add(1)
			go func() {
				defer wg.Done()
				metricFamilies, err := s.fetcher.Fetch(pod)
				if err != nil {
					klog.Errorf("failed to parse metrics from %s: %v", pod, err)
					return
				}

				s.refreshLoraMetrics(pod, metricFamilies)
				s.refreshRequestMetrics(pod, metricFamilies)
			}()
		}
		wg.Wait()
		time.Sleep(interval)
	}
}
