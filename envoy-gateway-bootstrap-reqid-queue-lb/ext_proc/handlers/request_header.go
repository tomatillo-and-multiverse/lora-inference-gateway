package handlers

import (
	"fmt"
	"log"

	configPb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	filterPb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

func HandleRequestHeaders(req *extProcPb.ProcessingRequest) *extProcPb.ProcessingResponse {
	log.Println("--- In RequestHeaders processing ...")
	r := req.Request
	h := r.(*extProcPb.ProcessingRequest_RequestHeaders)

	log.Printf("Headers: %+v\n", h)

	var resp *extProcPb.ProcessingResponse
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

	// Print final headers being sent
	fmt.Println("[request_header]Final headers being sent:")
	for _, header := range resp.GetRequestHeaders().GetResponse().GetHeaderMutation().GetSetHeaders() {
		fmt.Printf("%s: %s\n", header.GetHeader().Key, header.GetHeader().RawValue)
	}
	return resp
}
