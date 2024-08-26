package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	"github.com/coocood/freecache"

	"ext-proc/cache"
)

func getLatency(ReceivedTime string, SentTime string) time.Duration {
	receivedTime, err := time.Parse(time.RFC3339, ReceivedTime)
	if err != nil {
		log.Printf("Error parsing received time: %v", err)
	}
	sentTime, err := time.Parse(time.RFC3339, SentTime)
	if err != nil {
		log.Printf("Error parsing sent time: %v", err)
	}
	return receivedTime.Sub(sentTime)
}

func HandleResponseHeaders(req *extProcPb.ProcessingRequest, pods []string, ipPodMap map[string]string, cacheActiveLoraModel, cachePendingRequestActiveAdapters, cacheLLMRequests *freecache.Cache) *extProcPb.ProcessingResponse {
	log.Println("--- In ResponseHeaders processing")
	r := req.Request
	h := r.(*extProcPb.ProcessingRequest_ResponseHeaders)
	requestID := ""
	tokensSent := 0
	tokensReceived := 0
	targetPodIP := ""
	fmt.Printf("Headers: %+v\n", h)

	for _, n := range h.ResponseHeaders.Headers.Headers {
		if strings.ToLower(n.Key) == "x-request-id" {
			requestID = string(n.RawValue)
		}
		if strings.ToLower(n.Key) == "prompt_tokens" {
			var err error
			tokensSent, err = strconv.Atoi(string(n.RawValue))
			if err != nil {
				log.Printf("Error converting prompt_tokens: %v", err)
			} 
		}
		if strings.ToLower(n.Key) == "completion_tokens" {
			var err error
			tokensReceived, err = strconv.Atoi(string(n.RawValue))
			if err != nil {
				log.Printf("Error converting completion_tokens: %v", err)
			}  
		}
	}
	llmRequest, err := cache.GetCacheLLMRequests(cacheLLMRequests, requestID)
	latency := ""
	if err == nil {
		targetPodIP = llmRequest.IP
		fmt.Printf("fetched ip for req %s:%s\n", requestID, targetPodIP)
		llmRequest.TokensSent = tokensSent
		llmRequest.TokensReceived = tokensReceived
		llmRequest.TokensPending = 0
		llmRequest.ReceivedTime = time.Now().Format(time.RFC3339)
		llmRequest.Latency = getLatency(llmRequest.ReceivedTime, llmRequest.SentTime)
		latency = llmRequest.Latency.String()

		if err := cache.SetCacheLLMRequests(cacheLLMRequests, *llmRequest, 600); err != nil {
			log.Printf("Error setting cache llmRequest in Response Header: %v", err)
		}
	} else {
		log.Printf("llmRequest %s complete in cache for 600 seconds", requestID)
	}
	

	var loraMetrics []cache.ActiveLoraModelMetrics
	var requestMetrics []cache.PendingRequestActiveAdaptersMetrics
	var modelNames map[string]int
	pendingQueueSize := -1
	runningQueueSize := 0
	waitingQueueSize := 0
	podAdapterMap := make(map[string]int)
	GPUKVCacheUsagePerc := 0.0
	targetPod := ipPodMap[targetPodIP]
	
	for _, header := range h.ResponseHeaders.Headers.Headers {
		switch header.Key {
		case "active_lora_adapters":
			err := json.Unmarshal([]byte(header.RawValue), &modelNames)
			if err != nil {
				log.Printf("Error parsing model_names: %v", err)
			}
		case "pending_queue_size":
			var err error
			pendingQueueSize, err = strconv.Atoi(string(header.RawValue))
			if err != nil {
				log.Printf("Error converting pending_queue_size: %v", err)
			}
		case "waiting_queue_size":
			var err error
			waitingQueueSize, err = strconv.Atoi(string(header.RawValue))
			if err != nil {
				log.Printf("Error converting waiting_queue_size: %v", err)
			}
		case "running_queue_size:":
			var err error
			runningQueueSize, err = strconv.Atoi(string(header.RawValue))
			if err != nil {
				log.Printf("Error converting running_queue_size:: %v", err)
			}
		case "gpu_cache_usage_sys":
			var err error
			GPUKVCacheUsagePerc, err = strconv.ParseFloat(string(header.RawValue), 64)
			if err != nil {
				log.Printf("Error converting gpu_cache_usage_sys: %v", err)
			}
		}
	}
	if modelNames != nil {
		for modelName, numberOfPendingRequests := range modelNames {
			metric := cache.ActiveLoraModelMetrics{
				Date:                    time.Now().Format(time.RFC3339),
				PodName:                 targetPod,
				ModelName:               modelName,
				NumberOfPendingRequests: numberOfPendingRequests,
			}
			podAdapterMap[metric.PodName]++
			loraMetrics = append(loraMetrics, metric)
		}
		// Update cache with parsed values
		for _, metric := range loraMetrics {
			if err := cache.SetCacheActiveLoraModel(cacheActiveLoraModel, metric); err != nil {
				log.Printf("Error setting cache in Response Header: %v", err)
			}
		}
	}
	if pendingQueueSize >= 0 {
		baseModel, err := cache.GetBaseModel(cachePendingRequestActiveAdapters, targetPod)
		if err == nil {
			fmt.Printf("fetched baseModel for pod %s", targetPod)

		} else if err != freecache.ErrNotFound {
			log.Printf("Error fetching cachePendingRequestActiveAdapters for pod %s: %v", targetPod, err)
		}
		requestMetric := cache.PendingRequestActiveAdaptersMetrics{
			Date:                   time.Now().Format(time.RFC3339),
			PodName:                targetPod,
			PendingRequests:        pendingQueueSize,
			RunningRequests:        runningQueueSize,
			WaitingRequests:        waitingQueueSize,
			NumberOfActiveAdapters: podAdapterMap[targetPod],
			BaseModel:              baseModel,
			GPUKVCacheUsagePerc:    GPUKVCacheUsagePerc,
		}
		requestMetrics = append(requestMetrics, requestMetric)
		for _, metric := range requestMetrics {
			if err := cache.SetCachePendingRequestActiveAdapters(cachePendingRequestActiveAdapters, metric); err != nil {
				log.Printf("Error setting cache in Response Header: %v", err)
			}
		}
	}

	resp := &extProcPb.ProcessingResponse{
		Response: &extProcPb.ProcessingResponse_ResponseHeaders{
			ResponseHeaders: &extProcPb.HeadersResponse{
				Response: &extProcPb.CommonResponse{
					HeaderMutation: &extProcPb.HeaderMutation{
						SetHeaders: []*configPb.HeaderValueOption{
							{
								Header: &configPb.HeaderValue{
									Key:      "x-went-into-resp-headers",
									RawValue: []byte("true"),
								},
							},
							{
								Header: &configPb.HeaderValue{
									Key:      "target-pod",
									RawValue: []byte(targetPod),
								},
							},
							{
								Header: &configPb.HeaderValue{
									Key:      "latency",
									RawValue: []byte(latency),
								},
							},
						},
					},
				},
			},
		},
	}
	return resp
}
