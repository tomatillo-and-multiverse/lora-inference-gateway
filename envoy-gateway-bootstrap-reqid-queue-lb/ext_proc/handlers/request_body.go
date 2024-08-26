package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
	"strings"

	"github.com/coocood/freecache"
	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	envoyTypePb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	uuid "github.com/google/uuid"

	"ext-proc/cache"
	metrics "ext-proc/metrics"
	pQueue "ext-proc/redispriorityqueue"
)

func valueExists(m map[string]string, valueToFind string) bool {
	for _, value := range m {
		if value == valueToFind {
			return true
		}
	}
	return false
}

var tokens_to_word_count_ratio = 1.4
var targetTailLatencyInSec = 0.0
var targetTokensSentPerSec = 0.0


func HandleRequestBody(req *extProcPb.ProcessingRequest, pods []string, podIPMap map[string]string, ipPodMap map[string]string, cacheActiveLoraModel, cachePendingRequestActiveAdapters, cacheLLMRequests *freecache.Cache, reqIDs *[]string, pq *pQueue.RedisPriorityQueue, priorityMap map[string]int) *extProcPb.ProcessingResponse {
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
	prompt, ok := requestBody["prompt"].(string)
	if !ok {
		log.Println("prompt not found in request body")
		return nil
	}
	number_of_prompt_tokens := int(float64(len(strings.Fields(prompt)))*tokens_to_word_count_ratio)

	priority, exists := priorityMap[modelRequested]
	if !exists {
		priority = 1
	}
	item := &pQueue.Item{ID: requestID, Priority: priority}
	if err := pq.Push(item); err != nil {
		log.Printf("Error pushing item from the queue: %v", err)
	}

	// Improved loop to avoid infinite waiting
	// Use the scheduler's AddItemToQueue function
	if err := pQueue.AddItemToQueue(pq, item); err != nil {
		return nil
	}
	
	log.Printf("priority: %d", priority)

	metrics.FetchMetricsFromPods(pods, podIPMap, cacheActiveLoraModel, cachePendingRequestActiveAdapters)
	// Retrieve metrics from cache
	var loraMetrics []cache.ActiveLoraModelMetrics
	var requestMetrics []cache.PendingRequestActiveAdaptersMetrics
	loraAdapterRequested := ""
	baseModel := ""
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
			log.Printf("Error fetching cachePendingRequestActiveAdapters for BaseModel for pod %s : %v", pod, loraAdapterRequested, err)
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

		requestMetric, err := cache.GetCachePendingRequestActiveAdapters(cachePendingRequestActiveAdapters, pod)
		if err == nil {
			requestMetrics = append(requestMetrics, *requestMetric)
		} else if err != freecache.ErrNotFound {
			log.Printf("Error fetching cachePendingRequestActiveAdapters for pod %s: %v", pod, err)
			break
		}
	}
	var llmRequests []cache.LLMRequest
	for _, reqID := range *reqIDs {
		llmRequest, err := cache.GetCacheLLMRequests(cacheLLMRequests, reqID)
		if err == nil {
			llmRequests = append(llmRequests, *llmRequest)
			//fmt.Printf("fetched ip for req %s\n", reqID)
		} else {
			//log.Printf("Error fetching llmRequest for %s", reqID)
		}
	}

	fmt.Printf("Fetched loraMetrics: %+v\n", loraMetrics)
	fmt.Printf("Fetched requestMetrics: %+v\n", requestMetrics)


	targetPod = metrics.FindTargetPod(loraMetrics, requestMetrics, llmRequests, loraAdapterRequested, targetTailLatencyInSec, targetTokensSentPerSec)
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
		IP: 					  targetPodIP,
		PodName:				  targetPod,
	SentTime:                     time.Now().Format(time.RFC3339),
	ReceivedTime:                "",
	ReqID:		   			requestID,
	TokensSent:			    number_of_prompt_tokens, 
	TokensReceived:         0,
	TokensPending:			    int(max_tokens) + number_of_prompt_tokens, 
	ModelName:				modelRequested,
	BaseModel:				baseModel,
	Latency: time.Duration(0),
	}
	if err := cache.SetCacheLLMRequests(cacheLLMRequests, llmRequestMetric, 600); err != nil {
		log.Printf("Error setting llmRequestMetric for requestID %s: %v", requestID, err)
	}
	*reqIDs = append(*reqIDs, requestID)
	return resp
}
