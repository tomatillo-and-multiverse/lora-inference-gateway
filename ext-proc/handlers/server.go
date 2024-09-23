package handlers

import (
	"io"
	"time"

	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	klog "k8s.io/klog/v2"

	"ext-proc/backend"
	"ext-proc/scheduling"
)

func NewServer(pp PodProvider, scheduler Scheduler) *Server {
	return &Server{
		scheduler:       scheduler,
		podProvider:     pp,
		requestCtxStore: expirable.NewLRU[string, RequestContext](1024*1024*1024, nil, time.Second*600),
	}
}

type Server struct {
	scheduler   Scheduler
	podProvider PodProvider
	// requestCtxStore stores ongoing requests discoverable via request IDs so that request and response
	// handlers can access shared context.
	// We use an LRU cache so that stale entries can be cleared automatically.
	// Size and TTL must be set generously so that it doesn't accidentally expire entries that haven't
	// finished yet.
	requestCtxStore *expirable.LRU[string, RequestContext]
}

type Scheduler interface {
	Schedule(b *scheduling.LLMRequest) (targetPod *backend.Pod, targetModel string, err error)
}

// PodProvider is an interface to provide set of pods in the backend and information such as metrics.
type PodProvider interface {
	GetPodMetrics(pod backend.Pod) (*backend.PodMetrics, bool)
	UpdatePodMetrics(pod backend.Pod, pm *backend.PodMetrics)
}

func (s *Server) Process(srv extProcPb.ExternalProcessor_ProcessServer) error {
	klog.V(1).Info("Started process:  -->  ")
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

		klog.V(1).Info("Got stream:  -->  ")

		resp := &extProcPb.ProcessingResponse{}
		switch v := req.Request.(type) {
		case *extProcPb.ProcessingRequest_RequestBody:
			resp, err = s.HandleRequestBody(req)
		case *extProcPb.ProcessingRequest_ResponseHeaders:
			resp, err = s.HandleResponseHeaders(req)
		default:
			klog.Infof("Unknown Request type %+v", v)
		}

		if err != nil {
			return status.Errorf(codes.Unknown, "failed to handle request: %v", err)
		}

		if err := srv.Send(resp); err != nil {
			klog.Infof("send error %v", err)
		}
	}
}

// RequestContext stores context information of a request so that it can be accessible during
// request and response handling.
type RequestContext struct {
	ID        string
	TargetPod *backend.Pod
	Model     string
}

func (s *Server) AddRequestCtx(req RequestContext) {
	s.requestCtxStore.Add(req.ID, req)
}

func (s *Server) GetAndRemoveRequestCtx(reqID string) (RequestContext, bool) {
	rc, found := s.requestCtxStore.Get(reqID)
	s.requestCtxStore.Remove(reqID)
	return rc, found
}

func (s *Server) GetRequestCtx(reqID string) (RequestContext, bool) {
	return s.requestCtxStore.Get(reqID)
}

func (s *Server) RemoveRequestCtx(reqID string) {
	s.requestCtxStore.Remove(reqID)
}
