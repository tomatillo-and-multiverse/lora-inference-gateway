package handlers

import (
	"encoding/json"
	"fmt"
	"log"

	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	envoyTypePb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/coocood/freecache"

	"github.com/ekkinox/ext-proc-demo/ext-proc/cache"
	"github.com/ekkinox/ext-proc-demo/ext-proc/metrics"
)

func valueExists(m map[string]string, valueToFind string) bool {
	for _, value := range m {
		if value == valueToFind {
			return true
		}
	}
	return false
}

func HandleRequestBody(req *extProcPb.ProcessingRequest, pods []string, podIPMap map[string]string, ipPodMap map[string]string, cacheActiveLoraModel, cachePendingRequestActiveAdapters *freecache.Cache, targetPodIP string) (*extProcPb.ProcessingResponse, string) {
	log.Println("--- In RequestBody processing")
	var requestBody map[string]interface{}
	v := req.Request.(*extProcPb.ProcessingRequest_RequestBody)
	if err := json.Unmarshal(v.RequestBody.Body, &requestBody); err != nil {
		log.Printf("Error unmarshaling request body: %v", err)
		return nil, targetPodIP
	}

	loraAdapterRequested, ok := requestBody["model"].(string)
	if !ok {
		log.Println("model/lora-adapter not found in request body")
		return nil, targetPodIP
	}

	threshold := 100000
	thresholdValue, ok := requestBody["threshold"].(float64)
	if ok {
		threshold = int(thresholdValue)
	}
	targetPod := ""

	if targetPodIP == "" {
		// Retrieve metrics from cache
		var loraMetrics []cache.ActiveLoraModelMetrics
		var requestMetrics []cache.PendingRequestActiveAdaptersMetrics

		for _, pod := range pods {
			loraMetric, err := cache.GetCacheActiveLoraModel(cacheActiveLoraModel, pod, loraAdapterRequested)
			if err == nil {
				loraMetrics = append(loraMetrics, *loraMetric)
			} else if err != freecache.ErrNotFound {
				log.Printf("Error fetching cacheActiveLoraModel for pod %s and lora_adapter_requested %s: %v", pod, loraAdapterRequested, err)
			}

			requestMetric, err := cache.GetCachePendingRequestActiveAdapters(cachePendingRequestActiveAdapters, pod)
			if err == nil {
				requestMetrics = append(requestMetrics, *requestMetric)
			} else if err != freecache.ErrNotFound {
				log.Printf("Error fetching cachePendingRequestActiveAdapters for pod %s: %v", pod, err)
				break
			}
		}

		fmt.Printf("Fetched loraMetrics: %+v\n", loraMetrics)
		fmt.Printf("Fetched requestMetrics: %+v\n", requestMetrics)

		targetPod = metrics.FindTargetPod(loraMetrics, requestMetrics, loraAdapterRequested, threshold)
		targetPodIP = podIPMap[targetPod]
		fmt.Printf("Selected target pod: %s\n", targetPod)
		fmt.Printf("Selected target pod IP: %s\n", targetPodIP)
	} else {
		targetPod = ipPodMap[targetPodIP]
		fmt.Printf("Pre-selected target pod: %s\n", targetPod)
		fmt.Printf("Pre-selected target pod IP: %s\n", targetPodIP)
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
		}

		// Print headers
		for _, header := range headers {
			fmt.Printf("[request_body] Header Key: %s, Header Value: %s\n", header.Header.Key, header.Header.RawValue)
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
	}
	return resp, targetPodIP
}
