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
	 "github.com/go-redis/redis/v8"

	"ext-proc/handlers"

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
	cacheLLMRequests                  *freecache.Cache
	pods                              []string
	podIPMap                          map[string]string
	ipPodMap                          map[string]string
	interval                          = 30 * time.Second // Update interval for fetching metrics
	TTL                               = int64(7)
	pq                                *rpq.RedisPriorityQueue
	priorityMap 					map[string]int
	reqIDs                            *[]string
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
	flag.IntVar(&port, "port", 9002, "gRPC port")
	flag.StringVar(&certPath, "certPath", "", "path to extProcServer certificate and private key")
	podsFlag := flag.String("pods", "", "Comma-separated list of pod addresses")
	podIPsFlag := flag.String("podIPs", "", "Comma-separated list of pod IPs")
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


	rdb := redis.NewClient(&redis.Options{
		Addr: "redis:6379", // Use the Redis service name in Kubernetes
	})
	pq = rpq.NewRedisPriorityQueue(rdb, "my_priority_queue")

	priorityMap = map[string]int{
		"tweet-summary" : 1,
	}

	reqIDs = &[]string{}

	// cache init
	cacheActiveLoraModel = freecache.NewCache(1024)
	cachePendingRequestActiveAdapters = freecache.NewCache(1024)
	cacheLLMRequests = freecache.NewCache(1024 * 1024 * 1024)
	
	debug.SetGCPercent(20)

	// Start the periodic metrics fetching in a separate goroutine

	//go metrics.FetchMetricsPeriodically(pods, podIPMap, cacheActiveLoraModel, cachePendingRequestActiveAdapters, interval)
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
		CacheLLMRequests:                  cacheLLMRequests,
		PQ:                                pq,
		PriorityMap: 					   priorityMap,
		ReqIDs:                            reqIDs,			  
	})
	healthPb.RegisterHealthServer(s, &healthServer{})

	log.Println("Starting gRPC server on port :9002")

	// shutdown
	var gracefulStop = make(chan os.Signal)
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
