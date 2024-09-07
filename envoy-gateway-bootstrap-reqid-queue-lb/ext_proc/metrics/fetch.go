package metrics

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	"ext-proc/cache"
)

// Contains checks if a slice contains a specific element
func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// FetchLoraMetricsFromPod fetches metrics from a given pod and sends them to a channel
func FetchLoraMetricsFromPod(pod string, podIPMap map[string]string, ch chan<- []cache.ActiveLoraModelMetrics, wg *sync.WaitGroup) {
	defer wg.Done()
	ip, exists := podIPMap[pod]
	if !exists {
		log.Printf("pod %s has no corresponding ip defined", pod)
		return
	}
	url := fmt.Sprintf("http://%s/metrics", ip)
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("failed to fetch metrics from %s: %v", pod, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("unexpected status code from %s: %v", pod, resp.StatusCode)
		return
	}

	parser := expfmt.TextParser{}
	metricFamilies, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		log.Printf("failed to parse metrics from %s: %v", pod, err)
		return
	}

	var loraMetrics []cache.ActiveLoraModelMetrics
	var adapterList []string
	modelsDict := make(map[string]int)

	for name, mf := range metricFamilies {
		if name == "vllm:active_lora_adapters" {
			for _, m := range mf.GetMetric() {
				modelName := GetLabelValue(m, "active_lora_adapters")
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
			PodName:                 pod,
			ModelName:               modelName,
			NumberOfPendingRequests: numberOfPendingRequests,
		}
		loraMetrics = append(loraMetrics, loraMetric)
	}

	ch <- loraMetrics
}

// FetchRequestMetricsFromPod fetches request metrics from a given pod and sends them to a channel
func FetchRequestMetricsFromPod(pod string, podIPMap map[string]string, ch chan<- []cache.PendingRequestActiveAdaptersMetrics, wg *sync.WaitGroup) {
	defer wg.Done()

	ip, exists := podIPMap[pod]
	if !exists {
		log.Printf("pod %s has no corresponding ip defined", pod)
		return
	}
	url := fmt.Sprintf("http://%s/metrics", ip)
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("failed to fetch metrics from %s: %v", pod, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("unexpected status code from %s: %v", pod, resp.StatusCode)
		return
	}

	parser := expfmt.TextParser{}
	metricFamilies, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		log.Printf("failed to parse metrics from %s: %v", pod, err)
		return
	}

	var requestMetrics []cache.PendingRequestActiveAdaptersMetrics
	pendingRequests := 0
	runningRequests := 0
	waitingRequests := 0
	adapterCount := 0
	gpu_cache_usage_perc := 0.0
	prefill_latency_in_sec := 0.0
	e2e_latency_in_sec := 0.0
	baseModel := ""
	for name, mf := range metricFamilies {
		switch name {
		case "vllm:num_requests_waiting":
			for _, m := range mf.GetMetric() {
				pendingRequests += int(m.GetGauge().GetValue())
				runningRequests += int(m.GetGauge().GetValue())
			}
		case "vllm:num_requests_running":
			for _, m := range mf.GetMetric() {
				pendingRequests += int(m.GetGauge().GetValue())
				baseModel = m.GetLabel()[0].GetValue()
				waitingRequests += int(m.GetGauge().GetValue())
			}
		case "vllm:gpu_cache_usage_perc":
			for _, m := range mf.GetMetric() {
				gpu_cache_usage_perc += float64(m.GetGauge().GetValue())
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
		case "vllm:prefill_latency_in_sec":
			for _, m := range mf.GetMetric() {
				prefill_latency_in_sec += float64(m.GetGauge().GetValue())
			}
		case "vllm:e2e_latency_in_sec":
			for _, m := range mf.GetMetric() {
				e2e_latency_in_sec += float64(m.GetGauge().GetValue())
			}
		}
	}

	requestMetric := cache.PendingRequestActiveAdaptersMetrics{
		Date:                   time.Now().Format(time.RFC3339),
		PodName:                pod,
		PendingRequests:        pendingRequests,
		RunningRequests:        runningRequests,
		WaitingRequests:        waitingRequests,
		NumberOfActiveAdapters: adapterCount,
		BaseModel:              baseModel,
		GPUKVCacheUsagePerc:    gpu_cache_usage_perc,
	}
	requestMetrics = append(requestMetrics, requestMetric)

	ch <- requestMetrics
}

// FetchMetrics fetches metrics from all pods and returns them
func FetchMetrics(pods []string, podIPMap map[string]string) ([]cache.ActiveLoraModelMetrics, []cache.PendingRequestActiveAdaptersMetrics) {
	ch := make(chan []cache.ActiveLoraModelMetrics)
	ch2 := make(chan []cache.PendingRequestActiveAdaptersMetrics)
	var wg sync.WaitGroup
	var wg2 sync.WaitGroup

	for _, pod := range pods {
		wg.Add(1)
		go FetchLoraMetricsFromPod(pod, podIPMap, ch, &wg)
	}

	for _, pod := range pods {
		wg2.Add(1)
		go FetchRequestMetricsFromPod(pod, podIPMap, ch2, &wg2)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	go func() {
		wg2.Wait()
		close(ch2)
	}()

	var allLoraMetrics []cache.ActiveLoraModelMetrics
	var allRequestMetrics []cache.PendingRequestActiveAdaptersMetrics
	for loraMetrics := range ch {
		allLoraMetrics = append(allLoraMetrics, loraMetrics...)
	}
	for requestMetrics := range ch2 {
		allRequestMetrics = append(allRequestMetrics, requestMetrics...)
	}
	return allLoraMetrics, allRequestMetrics
}

// GetLabelValue returns the value of a label from a Prometheus metric
func GetLabelValue(m *io_prometheus_client.Metric, label string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == label {
			return l.GetValue()
		}
	}
	return ""
}
