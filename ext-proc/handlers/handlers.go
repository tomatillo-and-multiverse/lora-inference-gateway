package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	klog "k8s.io/klog/v2"

	"github.com/coocood/freecache"
	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	filterPb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	envoyTypePb "github.com/envoyproxy/go-control-plane/envoy/type/v3"

	"ext-proc/cache"
	"ext-proc/scheduling"
)

type Server struct {
	Pods                              map[string]*cache.Pod
	CacheActiveLoraModel              *freecache.Cache
	CachePendingRequestActiveAdapters *freecache.Cache
	TokenCache                        *scheduling.TokenCache
	EnforceFairness                   bool
}

func (s *Server) Process(srv extProcPb.ExternalProcessor_ProcessServer) error {
	klog.V(1).Info("Started process:  -->  ")
	ctx := srv.Context()
	targetPodIP := ""

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, err := srv.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "cannot receive stream request: %v", err)
		}

		klog.V(1).Info("Got stream:  -->  ")

		resp := &extProcPb.ProcessingResponse{}
		switch v := req.Request.(type) {
		case *extProcPb.ProcessingRequest_RequestHeaders:
			resp, targetPodIP = s.HandleRequestHeaders(req, targetPodIP)
		case *extProcPb.ProcessingRequest_RequestBody:
			resp, targetPodIP, err = s.HandleRequestBody(req, targetPodIP)
		case *extProcPb.ProcessingRequest_ResponseHeaders:
			resp, targetPodIP = s.HandleResponseHeaders(req, targetPodIP)
		default:
			klog.Info("Unknown Request type %+v\n", v)
		}

		if err != nil {
			klog.Errorf("Error processing request: %v", err)
			return status.Errorf(codes.Unknown, "failed to  process request: %v", err)
		}

		if err := srv.Send(resp); err != nil {
			klog.Info("send error %v", err)
			return status.Errorf(codes.Unknown, "failed to  send response back: %v", err)
		}
	}
}

func valueExists(m map[string]string, valueToFind string) bool {
	for _, value := range m {
		if value == valueToFind {
			return true
		}
	}
	return false
}

func (s *Server) HandleRequestBody(req *extProcPb.ProcessingRequest, targetPodIP string) (*extProcPb.ProcessingResponse, string, error) {
	var err error
	klog.V(2).Infof("--- In RequestBody processing: %v\n", targetPodIP)
	var requestBody map[string]interface{}
	v := req.Request.(*extProcPb.ProcessingRequest_RequestBody)
	if err := json.Unmarshal(v.RequestBody.Body, &requestBody); err != nil {
		klog.V(1).Infof("Error unmarshaling request body: %v", err)
		return nil, targetPodIP, fmt.Errorf("error unmarshaling request body: %v", err)
	}

	loraAdapterRequested, ok := requestBody["model"].(string)
	if !ok {
		klog.V(2).Info("model not found in request body")
		return nil, targetPodIP, fmt.Errorf("model not found in request")
	}

	klog.V(2).Infof("Model requested: %v", loraAdapterRequested)
	threshold := 100000
	thresholdValue, ok := requestBody["threshold"].(float64)
	if ok {
		threshold = int(thresholdValue)
	}
	var targetPod *cache.Pod

	if targetPodIP == "" {
		// Retrieve metrics from cache
		var loraMetrics []cache.ActiveLoraModelMetrics
		var requestMetrics []cache.PendingRequestActiveAdaptersMetrics

		for _, pod := range s.Pods {
			loraMetric, err := cache.GetCacheActiveLoraModel(s.CacheActiveLoraModel, *pod, loraAdapterRequested)
			if err == nil {
				loraMetrics = append(loraMetrics, *loraMetric)
			} else if err != freecache.ErrNotFound {
				klog.V(1).Infof("Error fetching cacheActiveLoraModel for pod %s and lora_adapter_requested %s: %v", pod, loraAdapterRequested, err)
			}

			requestMetric, err := cache.GetCachePendingRequestActiveAdapters(s.CachePendingRequestActiveAdapters, *pod)
			if err == nil {
				requestMetrics = append(requestMetrics, *requestMetric)
			} else if err != freecache.ErrNotFound {
				klog.V(1).Infof("Error fetching cachePendingRequestActiveAdapters for pod %s: %v", pod, err)
				break
			}
		}

		klog.V(2).Infof("Fetched loraMetrics: %+v\n", loraMetrics)
		klog.V(2).Infof("Fetched requestMetrics: %+v\n", requestMetrics)

		targetPod, err = findTargetPod(loraMetrics, requestMetrics, loraAdapterRequested, threshold)
		if err != nil {
			return nil, "", fmt.Errorf("failed to find target pod: %v", err)
		}
		targetPodIP = targetPod.Address
		klog.V(2).Infof("Selected target pod: %s\n", targetPod)
		klog.V(2).Infof("Selected target pod IP: %s\n", targetPodIP)
	} else {
		targetPod = s.Pods[targetPodIP]
		klog.V(2).Infof("Pre-selected target pod: %s\n", targetPod)
		klog.V(2).Infof("Pre-selected target pod IP: %s\n", targetPodIP)
	}

	var resp *extProcPb.ProcessingResponse
	if s.EnforceFairness && !s.TokenCache.IsFairRequest(loraAdapterRequested) {
		resp = &extProcPb.ProcessingResponse{
			Response: &extProcPb.ProcessingResponse_ImmediateResponse{
				ImmediateResponse: &extProcPb.ImmediateResponse{
					Status: &envoyTypePb.HttpStatus{
						Code: envoyTypePb.StatusCode_TooManyRequests,
					},
				},
			},
		}
	} else if _, ok := s.Pods[targetPodIP]; !ok {
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
			klog.V(2).Infof("[request_body] Header Key: %s, Header Value: %s\n", header.Header.Key, header.Header.RawValue)
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
	return resp, targetPodIP, nil
}

func (s *Server) HandleResponseHeaders(req *extProcPb.ProcessingRequest, targetPodIP string) (*extProcPb.ProcessingResponse, string) {
	klog.V(2).Info("--- In ResponseHeaders processing")
	r := req.Request
	h := r.(*extProcPb.ProcessingRequest_ResponseHeaders)

	klog.V(2).Infof("Headers: %+v\n", h)

	var loraMetrics []cache.ActiveLoraModelMetrics
	var requestMetrics []cache.PendingRequestActiveAdaptersMetrics
	var modelNames map[string]int
	var totalTokens int
	var model string
	var err error
	currentTime := time.Now().Unix()
	pendingQueueSize := -1
	podAdapterMap := make(map[cache.Pod]int)
	targetPod := s.Pods[targetPodIP]
	for _, header := range h.ResponseHeaders.Headers.Headers {
		switch header.Key {
		case "active_lora_adapters":
			err = json.Unmarshal([]byte(header.RawValue), &modelNames)
			if err != nil {
				klog.V(1).Infof("Error parsing model_names: %v", err)
			}
		case "pending_queue_size":
			var err error
			pendingQueueSize, err = strconv.Atoi(string(header.RawValue))
			if err != nil {
				klog.V(1).Infof("Error converting pending_queue_size: %v", err)
			}
		case "model":
			model = string(header.RawValue)
		case "total_tokens":
			totalTokens, err = strconv.Atoi(string(header.RawValue))
			if err != nil {
				klog.V(1).Infof("Error parsing total_tokens: %v", err)
			}
		}
	}
	if modelNames != nil {
		for modelName, numberOfPendingRequests := range modelNames {
			metric := cache.ActiveLoraModelMetrics{
				Date:                    time.Now().Format(time.RFC3339),
				Pod:                     *targetPod,
				ModelName:               modelName,
				NumberOfPendingRequests: numberOfPendingRequests,
			}
			podAdapterMap[metric.Pod]++
			loraMetrics = append(loraMetrics, metric)
		}
		klog.V(2).Infof("lora metric: %v", loraMetrics)

		// Update cache with parsed values
		for _, metric := range loraMetrics {
			if err := cache.SetCacheActiveLoraModel(s.CacheActiveLoraModel, metric); err != nil {
				klog.V(1).Infof("Error setting cache in Response Header: %v", err)
			}
		}
	}
	if pendingQueueSize >= 0 {
		requestMetric := cache.PendingRequestActiveAdaptersMetrics{
			Date:                   time.Now().Format(time.RFC3339),
			Pod:                    *targetPod,
			PendingRequests:        pendingQueueSize,
			NumberOfActiveAdapters: podAdapterMap[*targetPod],
		}
		requestMetrics = append(requestMetrics, requestMetric)
		for _, metric := range requestMetrics {
			if err := cache.SetCachePendingRequestActiveAdapters(s.CachePendingRequestActiveAdapters, metric); err != nil {
				klog.V(1).Infof("Error setting cache in Response Header: %v", err)
			}
		}
		klog.V(2).Infof("request metric: %v", requestMetrics)
	}
	klog.V(2).Infof("Model Value: %v", model)
	klog.V(2).Infof("Total Tokens: %v", totalTokens)
	if "model" != "" {
		s.TokenCache.StoreResponseInfo(model, currentTime, totalTokens)
	}
	s.TokenCache.AdapterMap.Range(func(k, v any) bool {
		klog.V(2).Infof("Adapter: %+v Entries: %+v", k, v)
		return true
	})

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
									RawValue: []byte(targetPod.Address),
								},
							},
						},
					},
				},
			},
		},
	}
	return resp, targetPod.Address
}

func (s *Server) HandleRequestHeaders(req *extProcPb.ProcessingRequest, targetPodIP string) (*extProcPb.ProcessingResponse, string) {
	klog.V(2).Info("--- In RequestHeaders processing ...")
	r := req.Request
	h := r.(*extProcPb.ProcessingRequest_RequestHeaders)

	klog.V(2).Infof("Headers: %+v\n", h)
	klog.V(2).Infof("EndOfStream: %v\n", h.RequestHeaders.EndOfStream)
	for _, n := range h.RequestHeaders.Headers.Headers {
		if strings.ToLower(n.Key) == "target-pod" {
			targetPodIP = string(n.RawValue)
		}
	}

	var resp *extProcPb.ProcessingResponse
	if targetPodIP == "" {
		bodyMode := filterPb.ProcessingMode_BUFFERED

		resp = &extProcPb.ProcessingResponse{
			Response: &extProcPb.ProcessingResponse_RequestHeaders{
				RequestHeaders: &extProcPb.HeadersResponse{
					Response: &extProcPb.CommonResponse{
						HeaderMutation: &extProcPb.HeaderMutation{
							SetHeaders: []*configPb.HeaderValueOption{
								{
									Header: &configPb.HeaderValue{
										Key:      "x-went-into-req-headers",
										RawValue: []byte("true"),
									},
								},
							},
						},
						ClearRouteCache: true,
					},
				},
			},
			ModeOverride: &filterPb.ProcessingMode{
				ResponseHeaderMode: filterPb.ProcessingMode_SEND,
				RequestBodyMode:    bodyMode,
			},
		}
	} else {
		bodyMode := filterPb.ProcessingMode_NONE

		resp = &extProcPb.ProcessingResponse{
			Response: &extProcPb.ProcessingResponse_RequestHeaders{
				RequestHeaders: &extProcPb.HeadersResponse{
					Response: &extProcPb.CommonResponse{
						HeaderMutation: &extProcPb.HeaderMutation{
							SetHeaders: []*configPb.HeaderValueOption{
								{
									Header: &configPb.HeaderValue{
										Key:      "x-went-into-req-headers",
										RawValue: []byte("true"),
									},
								},
								{
									Header: &configPb.HeaderValue{
										Key:      "target-pod",
										RawValue: []byte(targetPodIP),
									},
								},
							},
						},
						ClearRouteCache: true,
					},
				},
			},
			ModeOverride: &filterPb.ProcessingMode{
				ResponseHeaderMode: filterPb.ProcessingMode_SEND,
				RequestBodyMode:    bodyMode,
			},
		}
	}
	// Print final headers being sent
	klog.V(2).Info("[request_header]Final headers being sent:")
	for _, header := range resp.GetRequestHeaders().GetResponse().GetHeaderMutation().GetSetHeaders() {
		klog.V(2).Infof("%s: %s\n", header.GetHeader().Key, header.GetHeader().RawValue)
	}
	return resp, targetPodIP
}

// findTargetPod finds the target pod based on metrics and the requested lora adapter
func findTargetPod(loraMetrics []cache.ActiveLoraModelMetrics, requestMetrics []cache.PendingRequestActiveAdaptersMetrics, loraAdapterRequested string, threshold int) (*cache.Pod, error) {
	var targetPod *cache.Pod
	var bestAlternativePod *cache.Pod
	minAltRequests := math.MaxInt

	klog.V(2).Info("Searching for the best pod...")

	// Filter metrics for the requested model
	for _, reqMetric := range requestMetrics {
		if reqMetric.PendingRequests < minAltRequests {
			minAltRequests = reqMetric.PendingRequests
			bestAlternativePod = &reqMetric.Pod
		}
	}

	if loraAdapterRequested == "" && bestAlternativePod != nil {
		klog.V(2).Infof("Selected the best alternative pod: %s with %d pending requests\n", bestAlternativePod, minAltRequests)
		return bestAlternativePod, nil
	}

	var relevantMetrics []cache.ActiveLoraModelMetrics
	for _, metric := range loraMetrics {
		if metric.ModelName == loraAdapterRequested {
			relevantMetrics = append(relevantMetrics, metric)
		}
	}

	// If no metrics found for the requested model, choose the pod with the least active adapters randomly
	if len(relevantMetrics) == 0 {
		minActiveAdapters := math.MaxInt
		var podsWithLeastAdapters []cache.PendingRequestActiveAdaptersMetrics
		for _, reqMetric := range requestMetrics {
			if reqMetric.NumberOfActiveAdapters < minActiveAdapters {
				minActiveAdapters = reqMetric.NumberOfActiveAdapters
				podsWithLeastAdapters = []cache.PendingRequestActiveAdaptersMetrics{}
			}
			if reqMetric.NumberOfActiveAdapters == minActiveAdapters {
				podsWithLeastAdapters = append(podsWithLeastAdapters, reqMetric)
			}
		}

		if len(podsWithLeastAdapters) == 0 {
			return nil, fmt.Errorf("no pod with min adapter found")
		}
		rand.Seed(time.Now().UnixNano())
		targetPod = &podsWithLeastAdapters[rand.Intn(len(podsWithLeastAdapters))].Pod
		klog.V(2).Infof("Selected pod with the least active adapters: %s\n", targetPod)
		return targetPod, nil
	}

	// Find the pod with the max lora requests among the relevant metrics
	maxNumberOfPendingRequests := -1
	var bestPods []cache.ActiveLoraModelMetrics
	for _, metric := range relevantMetrics {
		if metric.ModelName == loraAdapterRequested {
			if metric.NumberOfPendingRequests > maxNumberOfPendingRequests {
				maxNumberOfPendingRequests = metric.NumberOfPendingRequests
				bestPods = []cache.ActiveLoraModelMetrics{}
			}
			if metric.NumberOfPendingRequests == maxNumberOfPendingRequests {
				bestPods = append(bestPods, metric)
			}
		}
	}

	if len(bestPods) > 0 {
		rand.Seed(time.Now().UnixNano())
		targetPod = &bestPods[rand.Intn(len(bestPods))].Pod
		klog.V(2).Infof("Selected pod with the highest NumberOfPendingRequests: %s\n", targetPod)
	} else {
		klog.V(2).Infof("No pods match the requested model: %s\n", loraAdapterRequested)
	}

	// If the number of active Lora adapters in the selected pod is greater than the threshold, choose the pod with the least requests
	if maxNumberOfPendingRequests > threshold && bestAlternativePod != nil {
		targetPod = bestAlternativePod
		klog.V(2).Infof("Selected pod's active Lora adapters exceed threshold, selecting the best alternative pod: %s with %d pending requests\n", targetPod, minAltRequests)
	}

	if targetPod == nil {
		return nil, fmt.Errorf("No pod found")
	}

	return targetPod, nil
}
