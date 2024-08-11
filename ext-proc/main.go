package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"ext-proc/cache"
	"ext-proc/handlers"
	"ext-proc/metrics"
	"ext-proc/scheduling"

	"github.com/coocood/freecache"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	klog "k8s.io/klog/v2"

	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	healthPb "google.golang.org/grpc/health/grpc_health_v1"
)

type extProcServer struct{}
type server struct{}

var (
	port                              int
	certPath                          string
	enforeFairness                    bool
	cacheActiveLoraModel              *freecache.Cache
	cachePendingRequestActiveAdapters *freecache.Cache
	podNames                          []string
	podIPMap                          map[string]string
	ipPodMap                          map[string]string
	interval                          = 30 * time.Second // Update interval for fetching metrics
	TTL                               = int64(7)
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
	flag.IntVar(&port, "port", 9002, "gRPC port")
	flag.StringVar(&certPath, "certPath", "", "path to extProcServer certificate and private key")
	enforceFairness := flag.Bool("enable-fairness", false, "flag to enable fairness enforcement over the KV-Cache")
	podsFlag := flag.String("pods", "", "Comma-separated list of pod addresses")
	podIPsFlag := flag.String("podIPs", "", "Comma-separated list of pod IPs")
	flag.Parse()

	if *podsFlag == "" || *podIPsFlag == "" {
		klog.Fatal("No pods or pod IPs provided. Use the -pods and -podIPs flags to specify comma-separated lists of pod addresses and pod IPs.")
	}

	podNames = strings.Split(*podsFlag, ",")
	podIPs := strings.Split(*podIPsFlag, ",")

	if len(podNames) != len(podIPs) {
		klog.Fatal("The number of pod addresses and pod IPs must match.")
	}

	fmt.Printf("Pods: %v", podNames)
	pods := make([]cache.Pod, 0, len(podNames))
	ipToPods := make(map[string]*cache.Pod, len(podIPs))
	for i, p := range podNames {
		pod := cache.Pod{
			Namespace: "default",
			Name:      p,
			Address:   podIPs[i],
		}
		pods = append(pods, pod)
		ipToPods[podIPs[i]] = &pod
	}

	// cache init
	cacheActiveLoraModel = freecache.NewCache(1024)
	cachePendingRequestActiveAdapters = freecache.NewCache(1024)
	debug.SetGCPercent(20)

	// Start the periodic metrics fetching in a separate goroutine
	fetcher := &metrics.PodMetrics{}
	store := metrics.NewStore(fetcher, cacheActiveLoraModel, cachePendingRequestActiveAdapters, pods)
	go store.FetchMetricsPeriodically(interval)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		klog.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()

	extProcPb.RegisterExternalProcessorServer(s, &handlers.Server{
		Pods:                              ipToPods,
		CacheActiveLoraModel:              cacheActiveLoraModel,
		CachePendingRequestActiveAdapters: cachePendingRequestActiveAdapters,
		TokenCache:                        scheduling.CreateNewTokenCache(TTL),
		EnforceFairness:                   *enforceFairness,
	})
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
