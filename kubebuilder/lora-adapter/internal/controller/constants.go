/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

const (
	loraServingBackendLabel = "inference.x-k8s.io/lora-backend"
	extProcContainer        = "us-docker.pkg.dev/kaushikmitra-gke-dev/kaushikmitra-docker-repo/grpc-vllm-lora-go:latest"
	extProcContainerPort    = 50051
	envoyContainerPort      = 8080
	modelServerPort         = "8000"
	staticEnvoyConfig       = `
    # Enable detailed logging
    layered_runtime:
      layers:
        - name: static_layer
          static_layer:
            envoy:
              logging:
                level: debug
    static_resources:
      listeners:
        - name: listener_0
          address:
            socket_address:
              address: 0.0.0.0
              port_value: 8080
          filter_chains:
            - filters:
                - name: envoy.filters.network.http_connection_manager
                  typed_config:
                    "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
                    stat_prefix: ingress_http
                    codec_type: AUTO
                    route_config:
                      name: local_route
                      virtual_hosts:      
                        - name: backend
                          domains: ["*"]
                          routes:
                            - match:
                                prefix: "/"
                              route:  
                                cluster_header: "target-pod"
                                timeout: 1000s  # Increase route timeout
                    http_filters:
                      - name: envoy.filters.http.ext_proc
                        typed_config:
                          "@type": type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor
                          failure_mode_allow: false
                          grpc_service:
                            envoy_grpc:
                              cluster_name: ext_proc_cluster
                          processing_mode:
                            request_header_mode: "SEND"
                            response_header_mode: "SEND"
                            request_body_mode: "BUFFERED"
                            response_body_mode: "NONE"
                            request_trailer_mode: "SKIP"
                            response_trailer_mode: "SKIP"
                      - name: envoy.filters.http.router
                        typed_config:
                          "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
      clusters:
`
	envoyClusterForPod = `        - name: {{.PodName}}
          connect_timeout: 1000s
          type: STRICT_DNS
          lb_policy: ROUND_ROBIN
          load_assignment:
            cluster_name: {{.PodName}}
            endpoints:
              - lb_endpoints:
                  - endpoint:
                      address:
                        socket_address:
                          address: {{.PodAddress}}
                          port_value: {{.PodPort}}
`
	envoyConfigForExtProc = `        - name: ext_proc_cluster
          connect_timeout: 1000s
          type: STRICT_DNS
          http2_protocol_options: {}
          lb_policy: ROUND_ROBIN
          load_assignment:
            cluster_name: ext_proc_cluster
            endpoints:
              - lb_endpoints:
                  - endpoint:
                      address:
                        socket_address:
                          address: {{.ExtProcAddress}}
                          port_value: {{.ExtProcPort}}
`
)
