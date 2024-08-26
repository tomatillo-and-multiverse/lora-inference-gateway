package metrics

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coocood/freecache"
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
		GPUKVCacheUsagePerc:   gpu_cache_usage_perc,
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

func EstimateKVCacheUtil(podMetrics *PodModelMetrics ) float64{
	maxTokensInGPUCache := 2810 * 16.0
	if (podMetrics.RunningRequests + podMetrics.WaitingRequests) == 0 {
		return 0.0
	}
	estimatedTokensInGPUKVCache := float64(podMetrics.RunningRequests) /(float64(podMetrics.RunningRequests) + float64(podMetrics.WaitingRequests) ) * float64(podMetrics.TokensPending)
	pseudoGPUKVCacheUsagePerc := estimatedTokensInGPUKVCache / (maxTokensInGPUCache )
	return pseudoGPUKVCacheUsagePerc
}

type PodModelMetrics struct {
	PodName              string
	ModelName			string
	Latencies            []time.Duration
	TokensSent           int
	TokensReceived       int
	TokensPending        int
	TokensRunning		int
	TokensWaiting		int
	RunningRequests int // pod level
	WaitingRequests int // pod level
	GPUKVCacheUsagePerc  float64 // pod level
}

// Calculate the 99th percentile of latencies
func calculatePercentile(latencies []time.Duration, percentile float64) time.Duration {
	if len(latencies) == 0 {
		return 0 // Consider handling this case differently if 0 is not a meaningful return value
	}
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})
	index := int(math.Ceil(float64(len(latencies)) * percentile)) - 1
	return latencies[index]
}

// FindTargetPod finds the target pod based on metrics and the requested Lora adapter
func FindTargetPod(loraMetrics []cache.ActiveLoraModelMetrics, requestMetrics []cache.PendingRequestActiveAdaptersMetrics, loraRequests []cache.LLMRequest, loraAdapterRequested string, targetTailLatencyInSec, targetTokensSentPerSec float64) string {

	fmt.Println("[FindTargetPod] Starting function")

	podModelMetricsMap := make(map[string]*PodModelMetrics)
	podMetricsMap := make(map[string]*PodModelMetrics)
	modelTokenMap := make(map[string]int)


	baseModel := ""
	// Loop through LLMRequest and calculate metrics per Pod
	for _, metric := range requestMetrics {
		baseModel = metric.BaseModel
		if podMetricsMap[metric.PodName] == nil {
			podMetricsMap[metric.PodName] = &PodModelMetrics{PodName: metric.PodName}
		}
		podMetricsMap[metric.PodName].GPUKVCacheUsagePerc = metric.GPUKVCacheUsagePerc
		podMetricsMap[metric.PodName].RunningRequests = metric.RunningRequests
		podMetricsMap[metric.PodName].WaitingRequests = metric.WaitingRequests
		key := baseModel + "+" + metric.PodName
		if podModelMetricsMap[key] == nil {
				podModelMetricsMap[key] = &PodModelMetrics{PodName: metric.PodName, ModelName: baseModel}
		}
		podModelMetricsMap[key].GPUKVCacheUsagePerc = podMetricsMap[metric.PodName].GPUKVCacheUsagePerc
		podModelMetricsMap[key].RunningRequests = podMetricsMap[metric.PodName].RunningRequests
		podModelMetricsMap[key].WaitingRequests = podMetricsMap[metric.PodName].WaitingRequests
	}

	for _, metric := range loraMetrics {
		modelName := metric.ModelName
		key := metric.ModelName + "+" + metric.PodName
		if podModelMetricsMap[key] == nil {
				podModelMetricsMap[key] = &PodModelMetrics{PodName: metric.PodName, ModelName: modelName}
		}
		podModelMetricsMap[key].GPUKVCacheUsagePerc = podMetricsMap[metric.PodName].GPUKVCacheUsagePerc
		podModelMetricsMap[key].RunningRequests = podMetricsMap[metric.PodName].RunningRequests
		podModelMetricsMap[key].WaitingRequests = podMetricsMap[metric.PodName].WaitingRequests
	}
	
		
	
	

	for _, req := range loraRequests {
		// Update metrics for the model+pod
		modelName := req.ModelName
		if modelName != "" {
			

			key := req.ModelName + "+" + req.PodName
			if podModelMetricsMap[key] == nil {
					podModelMetricsMap[key] = &PodModelMetrics{PodName: req.PodName, ModelName: modelName}
			}

			if req.ReceivedTime != "" {
				podModelMetricsMap[key].Latencies = append(podModelMetricsMap[key].Latencies, req.Latency)
			}
			podModelMetricsMap[key].TokensSent += req.TokensSent
			podModelMetricsMap[key].TokensReceived += req.TokensReceived
			podModelMetricsMap[key].TokensPending += req.TokensPending

			modelTokenMap[modelName] += req.TokensReceived
		}
		key := baseModel + "+" + req.PodName
		if podModelMetricsMap[key] == nil {
					podModelMetricsMap[key] = &PodModelMetrics{PodName: req.PodName, ModelName: modelName}
		}

		if req.ReceivedTime != "" {
				podModelMetricsMap[key].Latencies = append(podModelMetricsMap[key].Latencies, req.Latency)
		}
		podModelMetricsMap[key].TokensSent += req.TokensSent
		podModelMetricsMap[key].TokensReceived += req.TokensReceived
		podModelMetricsMap[key].TokensPending += req.TokensPending
		


		if podMetricsMap[req.PodName] == nil {
				podMetricsMap[req.PodName] = &PodModelMetrics{PodName: req.PodName}
		}
		podMetricsMap[req.PodName].Latencies = append(podMetricsMap[req.PodName].Latencies, req.Latency)
		podMetricsMap[req.PodName].TokensSent += req.TokensSent
		podMetricsMap[req.PodName].TokensReceived += req.TokensReceived
		podMetricsMap[req.PodName].TokensPending += req.TokensPending

		// Update model token count
		modelTokenMap[baseModel] += req.TokensReceived

		//fmt.Printf("[FindTargetPod] Updated pod model metrics: Model+Pod=%s, TokensSent=%d, TokensReceived=%d, TokensPending=%d\n", key, podModelMetricsMap[key].TokensSent, podModelMetricsMap[key].TokensReceived, podModelMetricsMap[key].TokensPending)
		//fmt.Printf("[FindTargetPod] Updated pod  metrics: Model=%s, TokensSent=%d, TokensReceived=%d, TokensPending=%d\n", req.PodName, podMetricsMap[req.PodName].TokensSent, podMetricsMap[req.PodName].TokensReceived, podMetricsMap[req.PodName].TokensPending)
	}
	
	

	var podsWithLora []string
	var podsWithLoraFound bool = false
	if loraAdapterRequested != "" {
		for _, metric := range loraMetrics {
			if metric.ModelName == loraAdapterRequested {
				podsWithLora = append(podsWithLora, metric.PodName)
				podsWithLoraFound = true
			}
		}
		fmt.Printf("[FindTargetPod] Pods with Lora adapter requested: %v\n", podsWithLora)
		if len(podsWithLora) == 0 {
			minAdapters := math.MaxInt
			for _, metric := range requestMetrics {
				if metric.NumberOfActiveAdapters < minAdapters {
					minAdapters = metric.NumberOfActiveAdapters
				}
			}
			for _, metric := range requestMetrics {
				if metric.NumberOfActiveAdapters == minAdapters {
					podsWithLora = append(podsWithLora, metric.PodName)
				}
			}
			fmt.Printf("[FindTargetPod] Pods with min Lora adapter loaded: %v\n", podsWithLora)
		}

	} else {
		for _, metric := range requestMetrics {
			podsWithLora = append(podsWithLora, metric.PodName)
		}
		fmt.Printf("[FindTargetPod] All pods considered: %v\n", podsWithLora)
	}

	var candidatePods []string
	if targetTailLatencyInSec > 0 {
		max99thLatency := time.Duration(0)
		for _, pod := range podsWithLora {
			key := ""
			if podsWithLoraFound == true {
				key = loraAdapterRequested + "+" + pod
			} else {
				key = baseModel + "+" + pod
			}
			latency99th := calculatePercentile(podModelMetricsMap[key].Latencies, 0.99)
			fmt.Printf("[FindTargetPod] key: %s, 99th percentile latency: %v\n", key, latency99th.Seconds())
			if latency99th.Seconds() < targetTailLatencyInSec && latency99th > max99thLatency {
				max99thLatency = latency99th
				candidatePods = []string{pod}
			} else if latency99th == max99thLatency {
				candidatePods = append(candidatePods, pod)
			}
		}
		fmt.Printf("[FindTargetPod] Candidate pods based on latency: %v\n", candidatePods)
	} else {
		modelName := loraAdapterRequested
		if modelName == "" && len(loraRequests) > 0 {
			modelName = loraRequests[0].BaseModel
		}

		tokensPer300 := float64(modelTokenMap[modelName]) / 300
		minKVCacheUtilization := 2.0
		maxKVCacheUtilization := 0.0


		for _, pod := range podsWithLora {
			minKVCacheUtilization = math.Min(podMetricsMap[pod].GPUKVCacheUsagePerc, minKVCacheUtilization)
			maxKVCacheUtilization = math.Max(podMetricsMap[pod].GPUKVCacheUsagePerc, maxKVCacheUtilization)
		}
		fmt.Printf("[FindTargetPod] Tokens per 300 seconds: %f, Min KVCache Utilization: %f, Max KVCache Utilization: %f\n", tokensPer300, minKVCacheUtilization, maxKVCacheUtilization)
		if tokensPer300 < targetTokensSentPerSec && (minKVCacheUtilization > 0.8){
		} else if (minKVCacheUtilization > 0.8) {
			for _, pod := range podsWithLora {
				if podMetricsMap[pod].GPUKVCacheUsagePerc == minKVCacheUtilization {
					candidatePods = append(candidatePods, pod)
				}
			}
			fmt.Printf("[FindTargetPod] Candidate pods based on min KVCache utilization: %v\n", candidatePods)
		} else {
			maxTokensPending := 0
			for _, pod := range podsWithLora {
				key := ""
				if podsWithLoraFound == true {
					key = loraAdapterRequested + "+" + pod
				} else {
					key = baseModel + "+" + pod
				}
				if (podModelMetricsMap[key].TokensPending == maxTokensPending && podModelMetricsMap[key].GPUKVCacheUsagePerc < 0.8) {
					candidatePods = append(candidatePods, pod)
				} else if podModelMetricsMap[key].TokensPending > maxTokensPending && podModelMetricsMap[key].GPUKVCacheUsagePerc < 0.8 {
					maxTokensPending = podModelMetricsMap[key].TokensPending
					candidatePods = []string{pod}
				}
			}
			fmt.Printf("[FindTargetPod] Candidate pods based on max tokens pending: %v\n", candidatePods)
		}

	}

	if len(candidatePods) > 0 {
		selectedPod := candidatePods[rand.Intn(len(candidatePods))]
		fmt.Printf("[FindTargetPod] Selected candidate pod: %s\n", selectedPod)
		return selectedPod
	}

	return selectPodWithMinPending(podMetricsMap)
}

// selectPodWithMinPending selects a pod with the minimum pending requests, randomizing if there are multiple
func selectPodWithMinPending(podMetricsMap map[string]*PodModelMetrics) string {
	minPending := math.MaxInt
	var minPendingPods []string
	for _, metrics := range podMetricsMap {
		if metrics.TokensPending < minPending {
			minPending = metrics.TokensPending
			minPendingPods = []string{metrics.PodName}
		} else if metrics.TokensPending == minPending {
			minPendingPods = append(minPendingPods, metrics.PodName)
		}
	}
	selectedPod := minPendingPods[rand.Intn(len(minPendingPods))]
	fmt.Printf("[FindTargetPod] Randomized among pods with min pending requests: %s\n", selectedPod)
	return selectedPod
}

// selectPodWithMinPendinga random pod
func selectRandomPod(podMetricsMap map[string]*PodModelMetrics) string {
	var allPods []string
	for _, metrics := range podMetricsMap {
		allPods = append(allPods, metrics.PodName)
	}
	selectedPod := allPods[rand.Intn(len(allPods))]
	fmt.Printf("[FindTargetPod] Randomized pod: %s\n", selectedPod)
	return selectedPod
}


// FetchMetricsPeriodically fetches metrics periodically and updates the cache
func FetchMetricsPeriodically(pods []string, podIPMap map[string]string, cacheActiveLoraModel *freecache.Cache, cachePendingRequestActiveAdapters *freecache.Cache, interval time.Duration) {
	for {
		loraMetrics, requestMetrics := FetchMetrics(pods, podIPMap)
		fmt.Printf("fetchMetricsPeriodically requestMetrics: %+v\n", requestMetrics)
		fmt.Printf("fetchMetricsPeriodically loraMetrics: %+v\n", loraMetrics)
		cacheActiveLoraModel.Clear()
		cachePendingRequestActiveAdapters.Clear()
		for _, metric := range loraMetrics {
			if err := cache.SetCacheActiveLoraModel(cacheActiveLoraModel, metric); err != nil {
				log.Printf("Error setting cache: %v", err)
			}
		}
		for _, metric := range requestMetrics {
			if err := cache.SetCachePendingRequestActiveAdapters(cachePendingRequestActiveAdapters, metric); err != nil {
				log.Printf("Error setting cache: %v", err)
			}
		}
		time.Sleep(interval)
	}
}

// FetchMetricsPeriodically fetches metrics periodically and updates the cache
func FetchMetricsFromPods(pods []string, podIPMap map[string]string, cacheActiveLoraModel *freecache.Cache, cachePendingRequestActiveAdapters *freecache.Cache) {

		loraMetrics, requestMetrics := FetchMetrics(pods, podIPMap)
		fmt.Printf("fetchMetricsPeriodically requestMetrics: %+v\n", requestMetrics)
		fmt.Printf("fetchMetricsPeriodically loraMetrics: %+v\n", loraMetrics)
		cacheActiveLoraModel.Clear()
		cachePendingRequestActiveAdapters.Clear()
		for _, metric := range loraMetrics {
			if err := cache.SetCacheActiveLoraModel(cacheActiveLoraModel, metric); err != nil {
				log.Printf("Error setting cache: %v", err)
			}
		}
		for _, metric := range requestMetrics {
			if err := cache.SetCachePendingRequestActiveAdapters(cachePendingRequestActiveAdapters, metric); err != nil {
				log.Printf("Error setting cache: %v", err)
			}
		}

}
