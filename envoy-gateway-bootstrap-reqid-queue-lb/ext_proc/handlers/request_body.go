package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/coocood/freecache"
	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	envoyTypePb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	uuid "github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"

	"ext-proc/cache"
	metrics "ext-proc/metrics"
	pQueue "ext-proc/redispriorityqueue"
)

var targetTokensSentPerSec = 0.0

func Schedule(reqID string, redisPQManager pQueue.RedisPriorityQueueManager, podUseCaseMetricsMap map[string]*cache.PodUseCaseMetrics, maxAllowedKVCachePerc float64, useCase string, priority int, weight float64) {
	item := redisPQManager.CreateItem(reqID, podUseCaseMetricsMap, useCase, priority, weight)
	err := redisPQManager.AddItemToQueue(item)
	if err != nil {
		log.Printf("Error adding item to queue: %v", err)
	}
	isTop, err := redisPQManager.WaitForTopItem(item)
	if err != nil {
		log.Printf("Error waiting for top item: %v", err)
	}
	if isTop {
		log.Printf("Item is at the top of the queue")
	}
}

func HandleRequestBody(req *extProcPb.ProcessingRequest, pods []string, podIPMap map[string]string, ipPodMap map[string]string, cacheActiveLoraModel, cachePendingRequestActiveAdapters, cachePodModelMetrics *freecache.Cache, lruCacheLLMRequests *expirable.LRU[string, cache.LLMRequest], pq *pQueue.RedisPriorityQueue, priorityMap map[string]int, maxAllowedKVCachePerc float64, redisPQManager *pQueue.RedisPriorityQueueManager) *extProcPb.ProcessingResponse {
	log.Println("--- In RequestBody processing")

	var requestBody map[string]interface{}
	v := req.Request.(*extProcPb.ProcessingRequest_RequestBody)
	requestID := uuid.New().String()
	targetPodIP := ""
	targetPod := ""

	if err := json.Unmarshal(v.RequestBody.Body, &requestBody); err != nil {
		log.Printf("Error unmarshaling request body: %v", err)
		return nil
	}

	modelRequested, ok := requestBody["model"].(string)
	if !ok {
		log.Println("model/lora-adapter not found in request body")
		return nil
	}

	max_tokens, ok := requestBody["max_tokens"].(float64)
	if !ok {
		log.Println("max_tokens not found in request body")
		max_tokens = 16 // default value for vLLM hardcoded
	}
	prompt_len, ok := requestBody["prompt_len"].(float64)
	if !ok {
		log.Println("prompt_len not found in request body")
		return nil
	}
	use_case, ok := requestBody["use_case"].(string)
	if !ok {
		log.Println("use_case not found in request body")
		return nil
	}

	//metrics.FetchMetricsFromPods(pods, podIPMap, cacheActiveLoraModel, cachePendingRequestActiveAdapters)
	// Retrieve metrics from cache
	var loraMetrics []cache.ActiveLoraModelMetrics
	var requestMetrics []cache.PendingRequestActiveAdaptersMetrics
	loraAdapterRequested := ""
	baseModel := ""

	for _, pod := range pods {
		requestMetric, err := cache.GetCachePendingRequestActiveAdapters(cachePendingRequestActiveAdapters, pod)
		if err == nil || err == freecache.ErrNotFound {
			requestMetrics = append(requestMetrics, *requestMetric)
		} else if err != freecache.ErrNotFound {
			log.Printf("Error fetching cachePendingRequestActiveAdapters for pod %s: %v", pod, err)
			break
		}
	}

	for _, pod := range pods {
		baseModelFromCache, err := cache.GetBaseModel(cachePendingRequestActiveAdapters, pod)
		baseModel = baseModelFromCache
		if err == nil {
			if modelRequested != baseModel {
				fmt.Printf("Base model: %s requested", baseModel)
				loraAdapterRequested = modelRequested
				break
			}
		} else if err != freecache.ErrNotFound {
			log.Printf("Error fetching cachePendingRequestActiveAdapters for BaseModel for pod %s : %v", pod, err)
		}
	}

	for _, pod := range pods {
		if loraAdapterRequested != "" {
			loraMetric, err := cache.GetCacheActiveLoraModel(cacheActiveLoraModel, pod, loraAdapterRequested)
			if err == nil {
				loraMetrics = append(loraMetrics, *loraMetric)
			} else if err != freecache.ErrNotFound {
				log.Printf("Error fetching cacheActiveLoraModel for pod %s and lora_adapter_requested %s: %v", pod, loraAdapterRequested, err)
			}
		}
	}

	var llmRequests []cache.LLMRequest
	for _, reqID := range lruCacheLLMRequests.Keys() {
		llmRequest, ok := cache.GetLRUCacheLLMRequest(lruCacheLLMRequests, reqID)
		if ok {
			llmRequests = append(llmRequests, *llmRequest)
			//fmt.Printf("fetched ip for req %s\n", reqID)
		} else {
			//log.Printf("Error fetching llmRequest for %s", reqID)
		}
	}

	fmt.Printf("Fetched loraMetrics: %+v\n", loraMetrics)
	fmt.Printf("Fetched requestMetrics: %+v\n", requestMetrics)

	podUseCaseMetricsMap := metrics.UpdatePodUseCaseMetrics(requestMetrics, llmRequests, cachePodModelMetrics, use_case, loraAdapterRequested, baseModel)

	if !metrics.IsCapacityAvailable(podUseCaseMetricsMap, maxAllowedKVCachePerc) {
		//Schedule(requestID, *redisPQManager, podUseCaseMetricsMap, maxAllowedKVCachePerc, use_case, priorityMap[use_case], 0)
		log.Printf("Capacity not available for use case %s", use_case)
	}

	targetPod = metrics.FindTargetPod(loraMetrics, requestMetrics, llmRequests, podUseCaseMetricsMap, loraAdapterRequested, use_case, baseModel, targetTokensSentPerSec, maxAllowedKVCachePerc)
	targetPodIP = podIPMap[targetPod]
	fmt.Printf("Selected target pod: %s\n", targetPod)
	fmt.Printf("Selected target pod IP: %s\n", targetPodIP)

	var resp *extProcPb.ProcessingResponse
	if !metrics.Contains(pods, targetPod) {
		resp = &extProcPb.ProcessingResponse{
			Response: &extProcPb.ProcessingResponse_ImmediateResponse{
				ImmediateResponse: &extProcPb.ImmediateResponse{
					Status: &envoyTypePb.HttpStatus{
						Code: envoyTypePb.StatusCode_NotFound,
					},
				},
			},
		}
	} else {
		headers := []*configPb.HeaderValueOption{
			{
				Header: &configPb.HeaderValue{
					Key:      "x-went-into-req-body",
					RawValue: []byte("true"),
				},
			},
			{
				Header: &configPb.HeaderValue{
					Key:      "target-pod",
					RawValue: []byte(targetPodIP),
				},
			},
			{
				Header: &configPb.HeaderValue{
					Key:      "x-request-id",
					RawValue: []byte(requestID),
				},
			},
		}

		resp = &extProcPb.ProcessingResponse{
			Response: &extProcPb.ProcessingResponse_RequestBody{
				RequestBody: &extProcPb.BodyResponse{
					Response: &extProcPb.CommonResponse{
						HeaderMutation: &extProcPb.HeaderMutation{
							SetHeaders: headers,
						},
					},
				},
			},
		}
		// Print headers being set
		for _, header := range headers {
			fmt.Printf("[request_body] Header Key: %s, Header Value: %s\n", header.Header.Key, string(header.Header.RawValue))
		}
	}
	llmRequestMetric := cache.LLMRequest{
		IP:                  targetPodIP,
		PodName:             targetPod,
		SentTime:            time.Now().Format(time.RFC3339),
		ReceivedTime:        "",
		ReqID:               requestID,
		TokensSent:          int(prompt_len),
		TokensReceived:      0,
		TokensPending:       int(max_tokens) + int(prompt_len),
		ModelName:           modelRequested,
		BaseModel:           baseModel,
		E2ELatencyInSec:     0.0,
		PrefillLatencyInSec: 0.0,
		UseCase:             use_case,
	}
	cache.SetLRUCacheLLMRequest(lruCacheLLMRequests, llmRequestMetric)

	return resp
}
