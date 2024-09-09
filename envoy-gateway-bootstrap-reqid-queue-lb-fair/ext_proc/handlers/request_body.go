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
)

var targetTokensSentPerSec = 0.0
var timeout = 300

func HandleRequestBody(req *extProcPb.ProcessingRequest, pods []string, podIPMap map[string]string, ipPodMap map[string]string, cacheActiveLoraModel, cachePendingRequestActiveAdapters *freecache.Cache, lruCacheLLMRequests, queuedRequests *expirable.LRU[string, cache.LLMRequest], poppedRequests *freecache.Cache, maxAllowedKVCachePerc float64, MaxTokensPerPod float64, verbose bool) *extProcPb.ProcessingResponse {
	if verbose {
		log.Println("--- In RequestBody processing")
	}

	var requestBody map[string]interface{}
	v := req.Request.(*extProcPb.ProcessingRequest_RequestBody)
	requestID := uuid.New().String()
	targetPodIP := ""
	targetPod := ""

	if err := json.Unmarshal(v.RequestBody.Body, &requestBody); err != nil {
		err = fmt.Errorf("error unmarshaling request body: %v", err)
		log.Println(err)
		return nil
	}

	modelRequested, ok := requestBody["model"].(string)
	if !ok {
		err := fmt.Errorf("model not found in request body")
		log.Println(err)
		return nil
	}

	max_tokens, ok := requestBody["max_tokens"].(float64)
	if !ok {
		max_tokens = 16 // default value for vLLM hardcoded
	}
	prompt_len, ok := requestBody["prompt_len"].(float64)
	if !ok {
		err := fmt.Errorf("prompt_len not found in request body")
		log.Println(err)
		return nil
	}
	use_case, ok := requestBody["use_case"].(string)
	if !ok {
		err := fmt.Errorf("use_case not found in request body")
		log.Println(err)
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
				if verbose {
					fmt.Printf("Base model: %s requested", baseModel)
				}
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

	if verbose {
		fmt.Printf("Fetched loraMetrics: %+v\n", loraMetrics)
		fmt.Printf("Fetched requestMetrics: %+v\n", requestMetrics)
	}

	podUseCaseMetricsMap := metrics.GetPodUseCaseMetrics(pods, cachePendingRequestActiveAdapters, lruCacheLLMRequests, use_case, loraAdapterRequested, verbose)
	max_tokens_allowed := int(MaxTokensPerPod * float64(len(pods))) // 2810 * 16 is the max tokens  per pod
	if metrics.TotalPendingTokens(lruCacheLLMRequests) > max_tokens_allowed {
		log.Printf("Capacity not available for reqID %s  use case %s", requestID, use_case)
		llmRequestMetric := cache.LLMRequest{
			IP:                  "",
			PodName:             "",
			SentTime:            "",
			ReceivedTime:        "",
			ReqID:               requestID,
			TokensSent:          int(prompt_len),
			TokensReceived:      0,
			TokensPending:       int(max_tokens) + int(prompt_len),
			ModelName:           "",
			BaseModel:           "",
			E2ELatencyInSec:     0.0,
			PrefillLatencyInSec: 0.0,
			UseCase:             use_case,
		}
		log.Printf("Adding request to queuedRequests for reqID %s", requestID)
		queuedRequests.Add(requestID, llmRequestMetric)

		time_in_queue := 0
		for {
			_, err := poppedRequests.Get([]byte(requestID))
			if err == nil {
				break
			}
			time.Sleep(1 * time.Second)
			time_in_queue += 1
			if time_in_queue > timeout {
				break
			}
		}
		if time_in_queue > timeout {
			resp := &extProcPb.ProcessingResponse{
				Response: &extProcPb.ProcessingResponse_ImmediateResponse{
					ImmediateResponse: &extProcPb.ImmediateResponse{
						Status: &envoyTypePb.HttpStatus{
							Code: envoyTypePb.StatusCode_TooManyRequests,
						},
					},
				},
			}
			return resp

		}
	}

	targetPod = metrics.FindTargetPod(loraMetrics, requestMetrics, podUseCaseMetricsMap, loraAdapterRequested, use_case, baseModel, targetTokensSentPerSec, maxAllowedKVCachePerc, verbose)
	targetPodIP = podIPMap[targetPod]
	if verbose {

		fmt.Printf("Selected target pod: %s\n", targetPod)
		fmt.Printf("Selected target pod IP: %s\n", targetPodIP)
	}

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
		if verbose {
			for _, header := range headers {
				fmt.Printf("[request_body] Header Key: %s, Header Value: %s\n", header.Header.Key, string(header.Header.RawValue))
			}
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
