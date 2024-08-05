package handlers

import (
	"encoding/json"
	"log"
	"strconv"
	"time"

	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	"github.com/coocood/freecache"

	"github.com/ekkinox/ext-proc-demo/ext-proc/cache"
)

func HandleResponseHeaders(req *extProcPb.ProcessingRequest, pods []string, ipPodMap map[string]string, cacheActiveLoraModel, cachePendingRequestActiveAdapters *freecache.Cache, targetPodIP string) (*extProcPb.ProcessingResponse, string) {
	log.Println("--- In ResponseHeaders processing")
	r := req.Request
	h := r.(*extProcPb.ProcessingRequest_ResponseHeaders)

	log.Printf("Headers: %+v\n", h)

	var loraMetrics []cache.ActiveLoraModelMetrics
	var requestMetrics []cache.PendingRequestActiveAdaptersMetrics
	var modelNames map[string]int
	pendingQueueSize := -1
	podAdapterMap := make(map[string]int)
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
		requestMetric := cache.PendingRequestActiveAdaptersMetrics{
			Date:                   time.Now().Format(time.RFC3339),
			PodName:                targetPod,
			PendingRequests:        pendingQueueSize,
			NumberOfActiveAdapters: podAdapterMap[targetPod],
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
						},
					},
				},
			},
		},
	}
	return resp, targetPod
}
