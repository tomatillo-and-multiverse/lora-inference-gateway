package metrics

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/coocood/freecache"
	"github.com/hashicorp/golang-lru/v2/expirable"

	"ext-proc/cache"
	// Add this line to import the package that contains the FetchMetrics function
)

// Calculate the 99th percentile of latencies
func calculatePercentile(latencies []float64, percentile float64) float64 {
	if len(latencies) == 0 {
		return 0 // Consider handling this case differently if 0 is not a meaningful return value
	}
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})
	index := int(math.Ceil(float64(len(latencies))*percentile)) - 1
	return latencies[index]
}

// determinePodsWithLora identifies the pods with the requested Lora adapter or those with minimum adapters if not found
func determinePodsWithLora(
	loraAdapterRequested string,
	loraMetrics []cache.ActiveLoraModelMetrics,
	requestMetrics []cache.PendingRequestActiveAdaptersMetrics,
	verbose bool,
) ([]string, bool) {
	var podsWithLora []string
	podsWithLoraFound := false

	// If a specific Lora adapter is requested, find the pods with that adapter
	if loraAdapterRequested != "" {
		for _, metric := range loraMetrics {
			if metric.ModelName == loraAdapterRequested {
				podsWithLora = append(podsWithLora, metric.PodName)
				podsWithLoraFound = true
			}
		}
		if verbose {
			fmt.Printf("[determinePodsWithLora] Pods with requested Lora adapter: %v\n", podsWithLora)
		}

		// If no pods with the requested adapter were found, select pods with the minimum adapters loaded
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
			if verbose {
				fmt.Printf("[determinePodsWithLora] Pods with minimum Lora adapters loaded: %v\n", podsWithLora)
			}
		}

	} else {
		// If no specific Lora adapter is requested, consider all pods
		for _, metric := range requestMetrics {
			podsWithLora = append(podsWithLora, metric.PodName)
		}
		if verbose {
			fmt.Printf("[determinePodsWithLora] All pods considered: %v\n", podsWithLora)
		}
	}

	return podsWithLora, podsWithLoraFound
}

// InitializePodUseCaseMetrics initializes the pod model metrics map
func InitializePodUseCaseMetrics(requestMetrics []cache.PendingRequestActiveAdaptersMetrics, useCase, loraAdapterRequested string) map[string]*cache.PodUseCaseMetrics {
	podUseCaseMetricsMap := make(map[string]*cache.PodUseCaseMetrics)
	for _, metric := range requestMetrics {
		baseModel := metric.BaseModel
		basekey := baseModel + ":" + metric.PodName
		if podUseCaseMetricsMap[basekey] == nil {
			podUseCaseMetricsMap[basekey] = &cache.PodUseCaseMetrics{PodName: metric.PodName, BaseModel: baseModel}
		}
		podUseCaseMetricsMap[basekey].GPUKVCacheUsagePerc = metric.GPUKVCacheUsagePerc
		key := useCase + ":" + metric.PodName
		if podUseCaseMetricsMap[key] == nil {
			podUseCaseMetricsMap[key] = &cache.PodUseCaseMetrics{PodName: metric.PodName, UseCase: useCase, ModelName: loraAdapterRequested, BaseModel: metric.BaseModel}
		}

	}
	return podUseCaseMetricsMap
}

// PopulatePodUseCaseMetrics populates the pod model metrics map with data from loraMetrics and loraRequests
func PopulatePodUseCaseMetrics(
	podUseCaseMetricsMap map[string]*cache.PodUseCaseMetrics,
	llmRequests []cache.LLMRequest,
) {

	for _, req := range llmRequests {
		basemodelkey := req.BaseModel + ":" + req.PodName
		if podUseCaseMetricsMap[basemodelkey] == nil {
			podUseCaseMetricsMap[basemodelkey] = &cache.PodUseCaseMetrics{PodName: req.PodName, BaseModel: req.BaseModel}
		}
		UpdateMetricsForBaseModel(podUseCaseMetricsMap, req)
		useCase := req.UseCase
		if useCase != "" {
			UpdateMetricsForUseCase(podUseCaseMetricsMap, req)
		}

	}
}

// updatePodUseCaseMetrics updates the pod model metrics for a specific request
func UpdateMetricsForUseCase(podUseCaseMetricsMap map[string]*cache.PodUseCaseMetrics, req cache.LLMRequest) {
	key := req.UseCase + ":" + req.PodName
	if podUseCaseMetricsMap[key] == nil {
		podUseCaseMetricsMap[key] = &cache.PodUseCaseMetrics{PodName: req.PodName, UseCase: req.UseCase, ModelName: req.ModelName, BaseModel: req.BaseModel}
	}

	if req.ReceivedTime != "" {
		podUseCaseMetricsMap[key].PrefillLatenciesInSecPerToken = append(podUseCaseMetricsMap[key].PrefillLatenciesInSecPerToken, (req.PrefillLatencyInSec)/float64(req.TokensSent))
		if req.TokensReceived > 1 {
			podUseCaseMetricsMap[key].DecodeLatenciesInSecPerToken = append(podUseCaseMetricsMap[key].DecodeLatenciesInSecPerToken, (req.E2ELatencyInSec-req.PrefillLatencyInSec)/float64(req.TokensReceived-1))
		}
		if req.TokensReceived > 0 {
			podUseCaseMetricsMap[key].E2ELatenciesInSecPerToken = append(podUseCaseMetricsMap[key].E2ELatenciesInSecPerToken, (req.E2ELatencyInSec)/float64(req.TokensReceived))
		}
	}
	podUseCaseMetricsMap[key].TokensSent += req.TokensSent
	podUseCaseMetricsMap[key].TokensReceived += req.TokensReceived
	podUseCaseMetricsMap[key].TokensPending += req.TokensPending
	podUseCaseMetricsMap[key].PseudoGPUKVCacheUsagePerc = float64(podUseCaseMetricsMap[key].TokensPending) / (2810 * 16.0)
}

// updateMetricsForRequest updates the metrics for each request
func UpdateMetricsForBaseModel(podUseCaseMetricsMap map[string]*cache.PodUseCaseMetrics, req cache.LLMRequest) {
	key := req.BaseModel + ":" + req.PodName
	if req.ReceivedTime != "" {
		podUseCaseMetricsMap[key].PrefillLatenciesInSecPerToken = append(podUseCaseMetricsMap[key].PrefillLatenciesInSecPerToken, (req.PrefillLatencyInSec)/float64(req.TokensSent))
		if req.TokensReceived > 1 {
			podUseCaseMetricsMap[key].DecodeLatenciesInSecPerToken = append(podUseCaseMetricsMap[key].DecodeLatenciesInSecPerToken, (req.E2ELatencyInSec-req.PrefillLatencyInSec)/float64(req.TokensReceived-1))
		}
		if req.TokensReceived > 0 {
			podUseCaseMetricsMap[key].E2ELatenciesInSecPerToken = append(podUseCaseMetricsMap[key].E2ELatenciesInSecPerToken, (req.E2ELatencyInSec)/float64(req.TokensReceived))
		}
	}
	podUseCaseMetricsMap[key].TokensSent += req.TokensSent
	podUseCaseMetricsMap[key].TokensReceived += req.TokensReceived
	podUseCaseMetricsMap[key].TokensPending += req.TokensPending
	if podUseCaseMetricsMap[key].TokensPending > 0 {
		podUseCaseMetricsMap[key].PseudoGPUKVCacheUsagePerc = float64(podUseCaseMetricsMap[key].TokensPending) / (2810 * 16.0)
	} else {
		podUseCaseMetricsMap[key].PseudoGPUKVCacheUsagePerc = podUseCaseMetricsMap[key].GPUKVCacheUsagePerc
	}
}

// DetermineCandidatePods determines the candidate pods based on metrics and constraints
func DetermineCandidatePods(
	podsWithLora []string,
	podUseCaseMetricsMap map[string]*cache.PodUseCaseMetrics,
	useCase string,
	targetTailLatencyInSecPerToken float64,
	maxAllowedKVCachePerc float64,
	verbose bool,
) []string {
	var candidatePods []string
	if targetTailLatencyInSecPerToken > 0 {
		max99thLatency := 0.0
		for _, pod := range podsWithLora {
			key := useCase + ":" + pod
			basekey := podUseCaseMetricsMap[key].BaseModel + ":" + pod
			latencyPerToken99th := calculatePercentile(podUseCaseMetricsMap[key].E2ELatenciesInSecPerToken, 0.99)
			if latencyPerToken99th < targetTailLatencyInSecPerToken && latencyPerToken99th > max99thLatency && podUseCaseMetricsMap[basekey].PseudoGPUKVCacheUsagePerc < maxAllowedKVCachePerc {
				max99thLatency = latencyPerToken99th
				candidatePods = []string{pod}
			} else if latencyPerToken99th == max99thLatency && podUseCaseMetricsMap[basekey].PseudoGPUKVCacheUsagePerc < maxAllowedKVCachePerc {
				candidatePods = append(candidatePods, pod)
			}
		}
	} else {
		candidatePods = determineBasedOnKVCache(podsWithLora, podUseCaseMetricsMap, useCase, maxAllowedKVCachePerc, verbose)
	}
	return candidatePods
}

// determineBasedOnKVCache determines candidate pods based on KV cache utilization
func determineBasedOnKVCache(
	podsWithLora []string,
	podUseCaseMetricsMap map[string]*cache.PodUseCaseMetrics,
	useCase string,
	maxAllowedKVCachePerc float64,
	verbose bool,
) []string {
	var candidatePods []string
	minKVCacheUtilization := 2.0
	maxKVCacheUtilization := 0.0
	for _, pod := range podsWithLora {
		key := useCase + ":" + pod
		basekey := podUseCaseMetricsMap[key].BaseModel + ":" + pod
		minKVCacheUtilization = math.Min(podUseCaseMetricsMap[basekey].PseudoGPUKVCacheUsagePerc, minKVCacheUtilization)
		maxKVCacheUtilization = math.Max(podUseCaseMetricsMap[basekey].PseudoGPUKVCacheUsagePerc, maxKVCacheUtilization)
	}
	if verbose {
		fmt.Printf("[FindTargetPod] Min KVCache Utilization: %f, Max KVCache Utilization: %f\n", minKVCacheUtilization, maxKVCacheUtilization)
	}
	if minKVCacheUtilization <= maxAllowedKVCachePerc {
		//maxTokensPending := 0
		maxGPUKVCacheUsagePerc := -1.0
		for _, pod := range podsWithLora {
			key := useCase + ":" + pod
			basekey := podUseCaseMetricsMap[key].BaseModel + ":" + pod
			if podUseCaseMetricsMap[key].PseudoGPUKVCacheUsagePerc == maxGPUKVCacheUsagePerc {
				candidatePods = append(candidatePods, pod)
			} else if podUseCaseMetricsMap[key].PseudoGPUKVCacheUsagePerc > maxGPUKVCacheUsagePerc && podUseCaseMetricsMap[basekey].PseudoGPUKVCacheUsagePerc < maxAllowedKVCachePerc {
				maxGPUKVCacheUsagePerc = podUseCaseMetricsMap[key].PseudoGPUKVCacheUsagePerc
				candidatePods = []string{pod}
			}
		}
		if verbose {
			fmt.Printf("[FindTargetPod] Candidate pods based on max tokens pending: %v with max token pending %v\n", candidatePods, maxGPUKVCacheUsagePerc)
		}
	}

	return candidatePods
}

// selectPodWithMinPending selects a pod with the minimum pending requests, randomizing if there are multiple
func selectPodWithMinPending(podUseCaseMetricsMap map[string]*cache.PodUseCaseMetrics, verbose bool) string {
	minKVCacheUtilization := 200.0
	var minPendingPods []string
	for key, metrics := range podUseCaseMetricsMap {
		if key != metrics.BaseModel+":"+metrics.PodName {
			continue
		}
		if metrics.PseudoGPUKVCacheUsagePerc < minKVCacheUtilization {
			minKVCacheUtilization = metrics.PseudoGPUKVCacheUsagePerc
			minPendingPods = []string{metrics.PodName}
		} else if metrics.PseudoGPUKVCacheUsagePerc == minKVCacheUtilization {
			minPendingPods = append(minPendingPods, metrics.PodName)
		}
	}
	selectedPod := minPendingPods[rand.Intn(len(minPendingPods))]

	if verbose {
		fmt.Printf("[FindTargetPod] Randomized among pods with min pending requests: %s\n", selectedPod)
	}
	return selectedPod
}

// SelectTargetPod selects the target pod from the list of candidates
func SelectTargetPod(candidatePods []string, podUseCaseMetricsMap map[string]*cache.PodUseCaseMetrics, baseModel string, verbose bool) string {
	candidatePodsMaximimPending := []string{}
	if len(candidatePods) > 0 {
		maxTokensPending := 0
		for _, pod := range candidatePods {
			key := baseModel + ":" + pod
			if podUseCaseMetricsMap[key].TokensPending == maxTokensPending {
				candidatePodsMaximimPending = append(candidatePodsMaximimPending, pod)
			} else if podUseCaseMetricsMap[key].TokensPending > maxTokensPending {
				maxTokensPending = podUseCaseMetricsMap[key].TokensPending
				candidatePodsMaximimPending = []string{pod}
			}
		}
		selectedPod := candidatePodsMaximimPending[rand.Intn(len(candidatePodsMaximimPending))]

		return selectedPod
	}
	return selectPodWithMinPending(podUseCaseMetricsMap, verbose)
}

func SetPodUseCaseMetrics(cachePodUseCaseMetrics *freecache.Cache, podUseCaseMetrics map[string]*cache.PodUseCaseMetrics) error {
	for key, metric := range podUseCaseMetrics {
		cache.SetPodUseCaseMetrics(cachePodUseCaseMetrics, *metric, key)
	}
	return nil
}

func TotalPendingTokens(lruCacheLLMRequests *expirable.LRU[string, cache.LLMRequest]) int {
	total_tokens_pending := 0
	for _, reqID := range lruCacheLLMRequests.Keys() {
		llmRequest, ok := cache.GetLRUCacheLLMRequest(lruCacheLLMRequests, reqID)
		if !ok {
			continue
		}
		total_tokens_pending += llmRequest.TokensPending
	}
	return (total_tokens_pending)
}

func GetPodUseCaseMetrics(pods []string, cachePendingRequestActiveAdapters *freecache.Cache, lruCacheLLMRequests *expirable.LRU[string, cache.LLMRequest], useCase, loraAdapterRequested string, verbose bool) map[string]*cache.PodUseCaseMetrics {
	var requestMetrics []cache.PendingRequestActiveAdaptersMetrics
	for _, pod := range pods {
		requestMetric, err := cache.GetCachePendingRequestActiveAdapters(cachePendingRequestActiveAdapters, pod)
		if err == nil || err == freecache.ErrNotFound {
			requestMetrics = append(requestMetrics, *requestMetric)
		} else if err != freecache.ErrNotFound {
			log.Printf("Error fetching cachePendingRequestActiveAdapters for pod %s: %v", pod, err)
			break
		}
	}
	podUseCaseMetricsMap := InitializePodUseCaseMetrics(requestMetrics, useCase, loraAdapterRequested)
	llmRequests := GetLLMRequestsFromLRU(lruCacheLLMRequests, verbose)
	PopulatePodUseCaseMetrics(podUseCaseMetricsMap, llmRequests)

	return podUseCaseMetricsMap
}

func GetLLMRequestsFromLRU(lruCacheLLMRequests *expirable.LRU[string, cache.LLMRequest], verbose bool) []cache.LLMRequest {
	var llmRequests []cache.LLMRequest
	for _, reqID := range lruCacheLLMRequests.Keys() {
		llmRequest, ok := cache.GetLRUCacheLLMRequest(lruCacheLLMRequests, reqID)
		if ok {
			llmRequests = append(llmRequests, *llmRequest)
			if verbose {
				fmt.Printf("fetched ip for req %s\n", reqID)
			}
		} else {
			if verbose {
			}
			log.Printf("[GetLLMRequestsFromLRU] Error fetching llmRequest for %s", reqID)
		}
	}
	return llmRequests
}

// FindTargetPod finds the target pod based on metrics and requested Lora adapter
func FindTargetPod(
	loraMetrics []cache.ActiveLoraModelMetrics,
	requestMetrics []cache.PendingRequestActiveAdaptersMetrics,
	podUseCaseMetricsMap map[string]*cache.PodUseCaseMetrics,
	loraAdapterRequested string,
	useCase string,
	baseModel string,
	targetTailLatencyInSecPerToken float64,
	maxAllowedKVCachePerc float64,
	verbose bool,
) string {

	podsWithLora, _ := determinePodsWithLora(loraAdapterRequested, loraMetrics, requestMetrics, verbose)

	candidatePods := DetermineCandidatePods(podsWithLora, podUseCaseMetricsMap, useCase, targetTailLatencyInSecPerToken, maxAllowedKVCachePerc, verbose)

	return SelectTargetPod(candidatePods, podUseCaseMetricsMap, baseModel, verbose)
}

// FetchMetricsPeriodically fetches metrics periodically and updates the cache
func FetchMetricsPeriodically(pods []string, podIPMap map[string]string, cacheActiveLoraModel *freecache.Cache, cachePendingRequestActiveAdapters *freecache.Cache, interval time.Duration, verbose bool) {
	for {
		loraMetrics, requestMetrics := FetchMetrics(pods, podIPMap)
		if verbose {

			fmt.Printf("fetchMetricsPeriodically requestMetrics: %+v\n", requestMetrics)
			fmt.Printf("fetchMetricsPeriodically loraMetrics: %+v\n", loraMetrics)
		}
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
