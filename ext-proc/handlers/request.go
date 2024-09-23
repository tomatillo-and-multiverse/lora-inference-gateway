package handlers

import (
	"encoding/json"
	"fmt"

	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/google/uuid"
	klog "k8s.io/klog/v2"

	"ext-proc/scheduling"
)

const (
	targetPodHeader = "target-pod"
	// We override the request ID because we cannot get the request id header from the body.
	// This may break tracing.
	// TODO: Figure out if it's possible to get request id header in request body processing.
	requestIDHeader = "x-request-id"
)

// HandleRequestBody handles body of the request to the backend server, such as parsing the "model"
// parameter.
// Envoy sends the request body to ext proc before sending the request to the backend server.
func (s *Server) HandleRequestBody(req *extProcPb.ProcessingRequest) (*extProcPb.ProcessingResponse, error) {
	klog.V(2).Infof("Handling request body")
	requestID := uuid.New().String()

	// Unmarshal request body (must be JSON).
	b, err := parseRequestBody(req)
	if err != nil {
		return nil, err
	}
	klog.V(2).Infof("Model requested: %v", b)

	targetPod, targetModel, err := s.scheduler.Schedule(b)
	if err != nil {
		return nil, fmt.Errorf("failed to find target pod")
	}
	klog.V(2).Infof("Selected target model %v in target pod: %v\n", targetModel, targetPod)

	// Store request context so that response handler can retrieve via the same request id.
	lrc := RequestContext{
		ID:        requestID,
		TargetPod: targetPod,
		Model:     b.Model,
	}
	s.AddRequestCtx(lrc)

	// Insert "target-pod" to instruct Envoy to route requests to the specified target pod.
	headers := []*configPb.HeaderValueOption{
		{
			Header: &configPb.HeaderValue{
				Key:      "x-went-into-req-body",
				RawValue: []byte("true"),
			},
		},
		{
			Header: &configPb.HeaderValue{
				Key:      targetPodHeader,
				RawValue: []byte(targetPod.Address),
			},
		},
		{
			Header: &configPb.HeaderValue{
				Key:      requestIDHeader,
				RawValue: []byte(requestID),
			},
		},
	}
	// Print headers for debugging
	for _, header := range headers {
		klog.V(2).Infof("[request_body] Header Key: %s, Header Value: %s\n", header.Header.Key, header.Header.RawValue)
	}

	resp := &extProcPb.ProcessingResponse{
		Response: &extProcPb.ProcessingResponse_RequestBody{
			RequestBody: &extProcPb.BodyResponse{
				Response: &extProcPb.CommonResponse{
					HeaderMutation: &extProcPb.HeaderMutation{
						SetHeaders: headers,
					},
					// TODO update target model in the body
					BodyMutation: &extProcPb.BodyMutation{},
				},
			},
		},
	}
	return resp, nil
}

// parseRequestBody unmarshals request body (must be JSON).
func parseRequestBody(req *extProcPb.ProcessingRequest) (*scheduling.LLMRequest, error) {
	// Unmarshal request body (must be JSON).
	var requestBody map[string]interface{}
	v := req.Request.(*extProcPb.ProcessingRequest_RequestBody)
	if err := json.Unmarshal(v.RequestBody.Body, &requestBody); err != nil {
		klog.V(1).Infof("Error unmarshaling request body: %v", err)
		return nil, fmt.Errorf("error unmarshaling request body: %v", err)
	}
	model, ok := requestBody["model"].(string)
	if !ok {
		return nil, fmt.Errorf("model not found in request")
	}
	klog.V(2).Infof("Model requested: %v", model)
	return &scheduling.LLMRequest{
		Model: model,
	}, nil
}
