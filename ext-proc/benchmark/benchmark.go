package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"runtime/debug"
	"time"

	"github.com/bojand/ghz/printer"
	"github.com/bojand/ghz/runner"
	"github.com/coocood/freecache"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/jhump/protoreflect/desc"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/proto"
	klog "k8s.io/klog/v2"

	"ext-proc/cache"
	"ext-proc/handlers"
	"ext-proc/metrics"
	"ext-proc/scheduling"
)

var (
	svrAddr               = flag.String("server_address", "localhost:9002", "Address of the grpc server")
	numPods               = flag.Int("num_pods", 200, "")
	numModelsPerPod       = flag.Int("num_models_per_pod", 5, "")
	localServer           = flag.Bool("local_server", true, "whether to start a local ext proc server")
	metricRefreshInterval = flag.Duration("metric_refresh_interval", time.Second, "")
	cacheSize             = flag.Int("cache_size", 100000, "size of the metrics cache")
	totalRequests         = flag.Int("total_requests", 10000, "number of requests to be sent for load test")
)

const (
	TTL  = int64(7)
	port = 9002
)

func main() {
	flag.Parse()

	if *localServer {
		go startExtProc()
		time.Sleep(time.Second) // wait until server is up
		klog.Info("Server started")
	}

	report, err := runner.Run(
		"envoy.service.ext_proc.v3.ExternalProcessor.Process",
		*svrAddr,
		runner.WithInsecure(true),
		runner.WithBinaryDataFunc(generateRequest),
		runner.WithTotalRequests(uint(*totalRequests)),
	)
	if err != nil {
		klog.Fatal(err)
	}

	printer := printer.ReportPrinter{
		Out:    os.Stdout,
		Report: report,
	}

	printer.Print("summary")
}

func generateRequest(mtd *desc.MethodDescriptor, callData *runner.CallData) []byte {
	numModels := *numPods * (*numModelsPerPod)
	j := map[string]interface{}{
		"model":       modelName(int(callData.RequestNumber) % numModels),
		"prompt":      "Write as if you were a critic: San Francisco",
		"max_tokens":  100,
		"temperature": 0,
	}

	llmReq, err := json.Marshal(j)
	if err != nil {
		klog.Fatal(err)
	}
	req := &extProcPb.ProcessingRequest{
		Request: &extProcPb.ProcessingRequest_RequestBody{
			RequestBody: &extProcPb.HttpBody{Body: llmReq},
		},
	}
	data, err := proto.Marshal(req)
	if err != nil {
		klog.Fatal("marshaling error: ", err)
	}
	return data
}

// startExtProc starts an extProc server with fake pods.
func startExtProc() {
	pods, ipToPods, fm := fakePods()
	// cache init
	cacheActiveLoraModel := freecache.NewCache(*cacheSize)
	cachePendingRequestActiveAdapters := freecache.NewCache(*cacheSize)
	debug.SetGCPercent(20)

	// Start the periodic metrics fetching in a separate goroutine
	fetcher := &metrics.Fake{Res: fm}
	store := metrics.NewStore(fetcher, cacheActiveLoraModel, cachePendingRequestActiveAdapters, pods)
	go store.FetchMetricsPeriodically(*metricRefreshInterval)

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
	})

	klog.Infof("Starting gRPC server on port :%v", port)
	reflection.Register(s)
	s.Serve(lis)
}

func fakePods() ([]cache.Pod, map[string]*cache.Pod, map[cache.Pod]map[string]*dto.MetricFamily) {
	pods := make([]cache.Pod, 0, *numPods)
	ipToPods := make(map[string]*cache.Pod, *numPods)
	metrics := make(map[cache.Pod]map[string]*dto.MetricFamily, *numPods)
	for i := 0; i < *numPods; i++ {
		address := fmt.Sprintf("address-%v", i)
		pod := cache.Pod{
			Namespace: "default",
			Name:      fmt.Sprintf("pod-%v", i),
			Address:   address,
		}
		pods = append(pods, pod)
		ipToPods[address] = &pod
		metrics[pod] = fakeMetrics(i)
	}

	return pods, ipToPods, metrics
}

// fakeMetrics adds numModelsPerPod number of adapters to the pod metrics.
func fakeMetrics(podNumber int) map[string]*dto.MetricFamily {
	metrics := make(map[string]*dto.MetricFamily)
	metrics["vllm:active_lora_adapters"] = &dto.MetricFamily{
		Metric: []*dto.Metric{},
	}
	metrics["vllm:info_active_adapters_info"] = &dto.MetricFamily{
		Metric: []*dto.Metric{
			{
				Label: []*dto.LabelPair{
					{
						Name:  ptrString("active_adapters"),
						Value: ptrString(""),
					},
				},
			},
		},
	}
	for i := 0; i < *numModelsPerPod; i++ {
		mn := modelName(podNumber*(*numModelsPerPod) + i)
		one := &dto.Metric{
			Label: []*dto.LabelPair{
				{
					Name:  ptrString("active_lora_adapters"),
					Value: ptrString(mn),
				},
			},
			Gauge: &dto.Gauge{Value: ptrFloat64(0)},
		}
		metrics["vllm:active_lora_adapters"].Metric = append(metrics["vllm:active_lora_adapters"].Metric, one)

		original := metrics["vllm:info_active_adapters_info"].Metric[0].Label[0].Value
		metrics["vllm:info_active_adapters_info"].Metric[0].Label[0].Value = ptrString(*original + "," + mn)
	}
	metrics["vllm:num_requests_waiting"] = &dto.MetricFamily{
		Metric: []*dto.Metric{
			{
				Gauge: &dto.Gauge{Value: ptrFloat64(0)},
			},
		},
	}
	metrics["vllm:num_requests_running"] = &dto.MetricFamily{
		Metric: []*dto.Metric{
			{
				Gauge: &dto.Gauge{Value: ptrFloat64(0)},
			},
		},
	}
	return metrics
}

func modelName(i int) string {
	return fmt.Sprintf("adapter-%v", i)
}

func ptrString(s string) *string {
	return &s
}

func ptrFloat64(f float64) *float64 {
	return &f
}
