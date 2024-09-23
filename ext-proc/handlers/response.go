package handlers

import (
	"encoding/json"
	"ext-proc/backend"
	"fmt"
	"strconv"
	"strings"

	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"go.uber.org/multierr"
	klog "k8s.io/klog/v2"
)

// HandleResponseHeaders processes response headers from the backend model server. It collects ORCA
// metrics from the response headers.
func (s *Server) HandleResponseHeaders(req *extProcPb.ProcessingRequest) (*extProcPb.ProcessingResponse, error) {
	klog.V(2).Info("Processing ResponseHeaders")
	h := req.Request.(*extProcPb.ProcessingRequest_ResponseHeaders)
	klog.V(2).Infof("Headers before: %+v\n", h)

	// Process response headers
	rid, found := getRequestID(h)
	if !found {
		return nil, fmt.Errorf("failed to get request ID from response headers")
	}
	_, ok := s.GetAndRemoveRequestCtx(rid)
	if !ok {
		return nil, fmt.Errorf("failed to get request context for %v", rid)
	}
	/* TODO: uncomment or remove the header based metrics update path below once we decide whether to do probing instead.
	existing, ok := s.podProvider.GetPodMetrics(*reqCtx.TargetPod)
	if !ok {
		existing = &backend.PodMetrics{}
	}
	probe metrics instead of using header.
	pm, err := headerToPodMetrics(h, *existing)
	if err != nil {
		// We just log the error and don't return the error because even a partial update of metrics
		// is preferred than not having any updates.
		klog.Errorf("failed to update pod metrics from response headers: %v", err)
	}
	s.podProvider.UpdatePodMetrics(*reqCtx.TargetPod, pm)
	*/

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
						},
					},
				},
			},
		},
	}
	return resp, nil
}

func getRequestID(h *extProcPb.ProcessingRequest_ResponseHeaders) (string, bool) {
	for _, header := range h.ResponseHeaders.Headers.Headers {
		if header.Key == requestIDHeader {
			return string(header.RawValue), true
		}
	}
	return "", false
}

// headerToPodMetrics updates pod metrics from response headers, it returns a new PodMetrics pointer
// which can be used to atomically update the pod metrics map.
func headerToPodMetrics(h *extProcPb.ProcessingRequest_ResponseHeaders, existing backend.PodMetrics) (pm *backend.PodMetrics, errs error) {
	updated := existing
	for _, header := range h.ResponseHeaders.Headers.Headers {
		switch strings.ToLower(header.Key) {
		case "active_lora_adapters":
			err := json.Unmarshal([]byte(header.RawValue), &updated.ActiveLoRAAdapters)
			multierr.Append(errs, err)
		case "waiting_queue_size":
			waitingQueueSize, err := strconv.Atoi(string(header.RawValue))
			multierr.Append(errs, err)
			if err != nil {
				updated.WaitingQueueSize = waitingQueueSize
			}
		case "running_queue_size:":
			runningQueueSize, err := strconv.Atoi(string(header.RawValue))
			multierr.Append(errs, err)
			if err != nil {
				updated.RunningQueueSize = runningQueueSize
			}
		case "gpu_cache_usage_sys":
			kvCacheUsagePercent, err := strconv.ParseFloat(string(header.RawValue), 64)
			multierr.Append(errs, err)
			if err != nil {
				updated.KVCacheUsagePercent = kvCacheUsagePercent
			}
		}
	}
	return &updated, errs
}
