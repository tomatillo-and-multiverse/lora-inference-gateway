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
	pods                              []string
	podIPMap                          map[string]string
	ipPodMap                          map[string]string
	interval                          = 30 * time.Second // Update interval for fetching metrics
	TTL                               = int64(7)
	wfqScheduler                      *rpq.Queue
	poppedRequests                    *freecache.Cache
	queuedRequests                    *expirable.LRU[string, cache.LLMRequest]
	maxTokensPerPod                   = 2810 * 16 * 1.6
	waitTimeBetweenScheduling         = 50 * time.Millisecond
)

type req struct {
	key    uint64
	size   uint64
	weight uint8
	id     string
}

// helper is a struct that helps with the priority queue
type wfqhelper struct{}

func (h *wfqhelper) Key(i interface{}) uint64 {
	return i.(*req).key
}

func (h *wfqhelper) Size(i interface{}) uint64 {
	return i.(*req).size
}

func (h *wfqhelper) Weight(i interface{}) uint8 {
	return i.(*req).weight
}

func (h *wfqhelper) ID(i interface{}) string {
	return i.(*req).id
}

// check lrucache if not empty put element in wfq
func putElementInWFQ(queuedItems *expirable.LRU[string, cache.LLMRequest], wfqScheduler *rpq.Queue) {
	// check if lruCache is not empty
	for {
		if queuedItems.Len() > 0 {
			log.Printf("Queued items: %v", queuedItems.Keys())
			for _, key := range queuedItems.Keys() {
				// get element from lruCache

				llmRequest, ok := queuedItems.Get(key)
				if !ok {
					log.Printf("Error fetching element from lruCache for key %s", key)
					continue
				}
				queuedItems.Remove(key)
				key := 2
				weight := 1
				if llmRequest.UseCase == "high-priority" {
					key = 0
					weight = 240
				} else if llmRequest.UseCase == "mid-priority" {
					key = 1
				}

				// create item
				item := &req{
					key:    uint64(key),
					size:   1,
					weight: uint8(weight),
					id:     llmRequest.ReqID,
				}
				// put item in wfq
				log.Printf("Putting item in wfq: %v", item)
				wfqScheduler.Queue(item)
			}
		} else {
			time.Sleep(1 * time.Second) // Sleep for 1 second before next scheduling iteration
		}
	}
}

// Get Elements from wfq and put in freecache
func getElementsFromWFQAndPutInCache(pods []string, lrucacheLLMRequests *expirable.LRU[string, cache.LLMRequest], wfqScheduler *rpq.Queue, poppedRequests *freecache.Cache) {
	for {
		// get element from wfq
		max_tokens_allowed := int(maxTokensPerPod * float64(len(pods))) // 2810 * 16 is the max tokens  per pod
		if metrics.TotalPendingTokens(lrucacheLLMRequests) > max_tokens_allowed {
			time.Sleep(1 * time.Second) // Sleep for 1 second before next scheduling iteration
			continue
		}
		item, ok := wfqScheduler.PeekOrDeQueue(true)
		if !ok {
			time.Sleep(1 * time.Second)
			continue
		}
		// put element in freecache
		err := poppedRequests.Set([]byte(item.(*req).id), []byte("1"), 0)
		if err != nil {
			log.Printf("Error setting element in poppedRequests for key %s: %v", item.(*req).id, err)
		} else {
			log.Printf("Successfully set element in poppedRequests for key %s of key %d", item.(*req).id, item.(*req).key)
		}
		time.Sleep(waitTimeBetweenScheduling)
	}
}

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

	poppedRequests = freecache.NewCache(1024)
	queuedRequests = expirable.NewLRU[string, cache.LLMRequest](1024*1024, nil, time.Second*600)

	// cache init
	cacheActiveLoraModel = freecache.NewCache(1024)
	cachePendingRequestActiveAdapters = freecache.NewCache(1024)
	lrucacheLLMRequests = expirable.NewLRU[string, cache.LLMRequest](1024*1024*1024, nil, time.Second*600)

	debug.SetGCPercent(20)

	wfqScheduler = rpq.NewQueue(10000, 500, &wfqhelper{})

	// Start the periodic metrics fetching in a separate goroutine

	go metrics.FetchMetricsPeriodically(pods, podIPMap, cacheActiveLoraModel, cachePendingRequestActiveAdapters, interval, verbose)

	go putElementInWFQ(queuedRequests, wfqScheduler)
	go getElementsFromWFQAndPutInCache(pods, lrucacheLLMRequests, wfqScheduler, poppedRequests)
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
		PoppedRequests:                    poppedRequests,
		QueuedRequests:                    queuedRequests,
		MaxAllowedKVCachePerc:             *maxAllowedKVCachePercent,
		MaxTokensPerPod:                   maxTokensPerPod,
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
