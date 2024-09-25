package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthPb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	klog "k8s.io/klog/v2"

	"ext-proc/backend"
	"ext-proc/handlers"
	"ext-proc/scheduling"
)

type extProcServer struct{}
type server struct{}

var (
	port       = flag.Int("port", 9002, "gRPC port")
	certPath   = flag.String("certPath", "", "path to extProcServer certificate and private key")
	podIPsFlag = flag.String("podIPs", "", "Comma-separated list of pod IPs")

	podIPMap map[string]string
	ipPodMap map[string]string
	interval = 30 * time.Second // Update interval for fetching metrics
	TTL      = int64(7)
)

type healthServer struct{}

func (s *healthServer) Check(ctx context.Context, in *healthPb.HealthCheckRequest) (*healthPb.HealthCheckResponse, error) {
	klog.Infof("Handling grpc Check request + %s", in.String())
	return &healthPb.HealthCheckResponse{Status: healthPb.HealthCheckResponse_SERVING}, nil
}

func (s *healthServer) Watch(in *healthPb.HealthCheckRequest, srv healthPb.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "Watch is not implemented")
}

func main() {
	flag.Parse()

	// Parse pod IPs.
	// TODO: Remove this once dynamic pod listing is implemented.
	if *podIPsFlag == "" {
		klog.Fatal("No pods or pod IPs provided. Use the -pods and -podIPs flags to specify comma-separated lists of pod addresses and pod IPs.")
	}
	podIPs := strings.Split(*podIPsFlag, ",")
	klog.Infof("Pods: %v", podIPs)
	pods := make(backend.PodSet)
	for _, ip := range podIPs {
		pod := backend.Pod{
			Namespace: "default",
			Name:      ip,
			Address:   ip,
		}
		pods[pod] = true
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		klog.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()

	pp := backend.NewProvider(&backend.PodMetricsClientImpl{}, &backend.FakePodLister{Pods: pods})
	extProcPb.RegisterExternalProcessorServer(s, handlers.NewServer(pp, scheduling.NewScheduler(pp)))
	healthPb.RegisterHealthServer(s, &healthServer{})

	klog.Infof("Starting gRPC server on port :%v", port)

	// shutdown
	var gracefulStop = make(chan os.Signal)
	signal.Notify(gracefulStop, syscall.SIGTERM)
	signal.Notify(gracefulStop, syscall.SIGINT)
	go func() {
		sig := <-gracefulStop
		klog.Infof("caught sig: %+v", sig)
		os.Exit(0)
	}()

	s.Serve(lis)

}
