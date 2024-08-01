package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"ext-proc/handlers"
	"ext-proc/metrics"
	"ext-proc/scheduling"

	"github.com/coocood/freecache"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc/credentials"
	healthPb "google.golang.org/grpc/health/grpc_health_v1"
)

type extProcServer struct{}
type server struct{}

var (
	port                              int
	certPath                          string
	cacheActiveLoraModel              *freecache.Cache
	cachePendingRequestActiveAdapters *freecache.Cache
	pods                              []string
	podIPMap                          map[string]string
	ipPodMap                          map[string]string
	interval                          = 30 * time.Second // Update interval for fetching metrics
	TTL                               = int64(7)
)

type healthServer struct{}

func (s *healthServer) Check(ctx context.Context, in *healthPb.HealthCheckRequest) (*healthPb.HealthCheckResponse, error) {
	log.Printf("Handling grpc Check request + %s", in.String())
	return &healthPb.HealthCheckResponse{Status: healthPb.HealthCheckResponse_SERVING}, nil
}

func (s *healthServer) Watch(in *healthPb.HealthCheckRequest, srv healthPb.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "Watch is not implemented")
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	certPool, err := loadCA(certPath)
	if err != nil {
		log.Fatalf("Could not load CA certificate: %v", err)
	}

	// Create TLS configuration
	tlsConfig := &tls.Config{
		RootCAs:    certPool,
		ServerName: "grpc-ext-proc.envoygateway",
	}

	// Create gRPC dial options
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	}

	conn, err := grpc.Dial("localhost:9002", opts...)
	if err != nil {
		log.Fatalf("Could not connect: %v", err)
	}
	_ = conn
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

	// cache init
	cacheActiveLoraModel = freecache.NewCache(1024)
	cachePendingRequestActiveAdapters = freecache.NewCache(1024)
	debug.SetGCPercent(20)

	// Start the periodic metrics fetching in a separate goroutine

	go metrics.FetchMetricsPeriodically(pods, podIPMap, cacheActiveLoraModel, cachePendingRequestActiveAdapters, interval)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	creds, err := loadTLSCredentials(certPath)
	if err != nil {
		log.Fatalf("Failed to load TLS credentials: %v", err)
	}

	s := grpc.NewServer(grpc.Creds(creds))

	extProcPb.RegisterExternalProcessorServer(s, &handlers.Server{
		Pods:                              pods,
		PodIPMap:                          podIPMap,
		IpPodMap:                          ipPodMap,
		CacheActiveLoraModel:              cacheActiveLoraModel,
		CachePendingRequestActiveAdapters: cachePendingRequestActiveAdapters,
		TokenCache:                        scheduling.CreateNewTokenCache(int64(7)),
	})
	healthPb.RegisterHealthServer(s, &healthServer{})

	log.Println("Starting gRPC server on port :9002")

	go func() {
		err = s.Serve(lis)
		if err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	http.HandleFunc("/healthz", healthCheckHandler)
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
func loadTLSCredentials(certPath string) (credentials.TransportCredentials, error) {
	// Load extProcServer's certificate and private key
	crt := "server.crt"
	key := "server.key"

	if certPath != "" {
		if !strings.HasSuffix(certPath, "/") {
			certPath = fmt.Sprintf("%s/", certPath)
		}
		crt = fmt.Sprintf("%s%s", certPath, crt)
		key = fmt.Sprintf("%s%s", certPath, key)
	}
	certificate, err := tls.LoadX509KeyPair(crt, key)
	if err != nil {
		return nil, fmt.Errorf("could not load extProcServer key pair: %s", err)
	}

	// Create a new credentials object
	creds := credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{certificate}})

	return creds, nil
}

func loadCA(caPath string) (*x509.CertPool, error) {
	ca := x509.NewCertPool()
	caCertPath := "server.crt"
	if caPath != "" {
		if !strings.HasSuffix(caPath, "/") {
			caPath = fmt.Sprintf("%s/", caPath)
		}
		caCertPath = fmt.Sprintf("%s%s", caPath, caCertPath)
	}
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("could not read ca certificate: %s", err)
	}
	ca.AppendCertsFromPEM(caCert)
	return ca, nil
}
