package handlers

import (
	"io"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	"github.com/hashicorp/golang-lru/v2/expirable"

	"github.com/coocood/freecache"

	"ext-proc/cache"
	"ext-proc/redispriorityqueue"
)

type Server struct {
	Pods                              []string
	PodIPMap                          map[string]string
	IpPodMap                          map[string]string
	CacheActiveLoraModel              *freecache.Cache
	CachePendingRequestActiveAdapters *freecache.Cache
	LRUCacheLLMRequests               *expirable.LRU[string, cache.LLMRequest]
	CachePodModelMetrics              *freecache.Cache
	PriorityMap                       map[string]int
	WFQScheduler                      *redispriorityqueue.WFQScheduler
	MaxAllowedKVCachePerc             float64
	Verbose                           bool
}

func (s *Server) Process(srv extProcPb.ExternalProcessor_ProcessServer) error {
	if s.Verbose {
		log.Println(" ")
		log.Println(" ")
		log.Println("Started process:  -->  ")
	}

	ctx := srv.Context()

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
		if s.Verbose {
			log.Println(" ")
			log.Println(" ")
			log.Println("Got stream:  -->  ")
		}

		resp := &extProcPb.ProcessingResponse{}
		switch v := req.Request.(type) {
		case *extProcPb.ProcessingRequest_RequestHeaders:
			resp = HandleRequestHeaders(req, s.Verbose)
		case *extProcPb.ProcessingRequest_RequestBody:
			resp = HandleRequestBody(req, s.Pods, s.PodIPMap, s.IpPodMap, s.CacheActiveLoraModel, s.CachePendingRequestActiveAdapters, s.CachePodModelMetrics, s.LRUCacheLLMRequests, s.PriorityMap, s.MaxAllowedKVCachePerc, s.WFQScheduler, s.Verbose)
		case *extProcPb.ProcessingRequest_ResponseHeaders:
			resp = HandleResponseHeaders(req, s.Pods, s.IpPodMap, s.CacheActiveLoraModel, s.CachePendingRequestActiveAdapters, s.LRUCacheLLMRequests, s.Verbose)
		default:
			log.Printf("Unknown Request type %+v\n", v)
		}
		if err := srv.Send(resp); err != nil {
			log.Printf("send error %v", err)
		}
	}
}
