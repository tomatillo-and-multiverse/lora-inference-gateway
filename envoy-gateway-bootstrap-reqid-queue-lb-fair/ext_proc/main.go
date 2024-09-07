// Remove the duplicate module declaration

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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

	"github.com/hashicorp/golang-lru/v2/expirable"

	rpq "ext-proc/redispriorityqueue"

	"github.com/coocood/freecache"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	healthPb "google.golang.org/grpc/health/grpc_health_v1"
)

type extProcServer struct{}
type server struct{}

var (
	port                              int
	certPath                          string
	cacheActiveLoraModel              *freecache.Cache
	cachePendingRequestActiveAdapters *freecache.Cache
	lrucacheLLMRequests               *expirable.LRU[string, cache.LLMRequest]
	cachePodModelMetrics              *freecache.Cache
	pods                              []string
	podIPMap                          map[string]string
	ipPodMap                          map[string]string
	interval                          = 30 * time.Second // Update interval for fetching metrics
	TTL                               = int64(7)
	wfqScheduler                      *rpq.WFQScheduler
	priorityMap                       map[string]int
)

type healthServer struct{}

func (s *healthServer) Check(ctx context.Context, in *healthPb.HealthCheckRequest) (*healthPb.HealthCheckResponse, error) {
	log.Printf("Handling grpc Check request + %s", in.String())
	return &healthPb.HealthCheckResponse{Status: healthPb.HealthCheckResponse_SERVING}, nil
}

func (s *healthServer) Watch(in *healthPb.HealthCheckRequest, srv healthPb.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "Watch is not implemented")
}

func main() {
	verbose := false
	flag.IntVar(&port, "port", 9002, "gRPC port")
	flag.StringVar(&certPath, "certPath", "", "path to extProcServer certificate and private key")
	podsFlag := flag.String("pods", "", "Comma-separated list of pod addresses")
	podIPsFlag := flag.String("podIPs", "", "Comma-separated list of pod IPs")
	verboseFlag := flag.String("verbose", "False", "Is verbose")

	if strings.ToLower(*verboseFlag) == "true" {
		verbose := true
		log.Printf("Verbose: %v", verbose)
	}
	maxAllowedKVCachePercent := flag.Float64("maxAllowedKVCachePercent", 0.8, "Max allowed percentage of cache to be used")
	flag.Parse()

	if *podsFlag == "" || *podIPsFlag == "" {
		log.Fatal("No pods or pod IPs provided. Use the -pods and -podIPs flags to specify comma-separated lists of pod addresses and pod IPs.")
	}

	pods = strings.Split(*podsFlag, ",")
	podIPs := strings.Split(*podIPsFlag, ",")

	if len(pods) != len(podIPs) {
		log.Fatal("The number of pod addresses and pod IPs must match.")
	}

	podIPMap = make(map[string]string)
	for i := range pods {
		podIPMap[pods[i]] = podIPs[i]
	}
	ipPodMap = make(map[string]string)
	for i := range podIPs {
		ipPodMap[podIPs[i]] = pods[i]
	}

	wfqScheduler = rpq.NewWFQScheduler()

	priorityMap = map[string]int{
		"tweet-summary": 1,
	}

	// cache init
	cacheActiveLoraModel = freecache.NewCache(1024)
	cachePendingRequestActiveAdapters = freecache.NewCache(1024)
	lrucacheLLMRequests = expirable.NewLRU[string, cache.LLMRequest](1024*1024*1024, nil, time.Second*600)
	cachePodModelMetrics = freecache.NewCache(1024)

	debug.SetGCPercent(20)

	// Start the periodic metrics fetching in a separate goroutine

	go metrics.FetchMetricsPeriodically(pods, podIPMap, cacheActiveLoraModel, cachePendingRequestActiveAdapters, interval, verbose)
	//qc.Start()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()

	extProcPb.RegisterExternalProcessorServer(s, &handlers.Server{
		Pods:                              pods,
		PodIPMap:                          podIPMap,
		IpPodMap:                          ipPodMap,
		CacheActiveLoraModel:              cacheActiveLoraModel,
		CachePendingRequestActiveAdapters: cachePendingRequestActiveAdapters,
		LRUCacheLLMRequests:               lrucacheLLMRequests,
		CachePodModelMetrics:              cachePodModelMetrics,
		PriorityMap:                       priorityMap,
		WFQScheduler:                      wfqScheduler,
		MaxAllowedKVCachePerc:             *maxAllowedKVCachePercent,
		Verbose:                           verbose,
	})
	healthPb.RegisterHealthServer(s, &healthServer{})

	log.Println("Starting gRPC server on port :9002")

	// shutdown
	var gracefulStop = make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM)
	signal.Notify(gracefulStop, syscall.SIGINT)
	go func() {
		sig := <-gracefulStop
		log.Printf("caught sig: %+v", sig)
		log.Println("Wait for 1 second to finish processing")
		time.Sleep(1 * time.Second)
		os.Exit(0)
	}()

	s.Serve(lis)

}
