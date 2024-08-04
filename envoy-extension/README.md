
# LoRA Inference Gateway

## Overview

This project contains the necessary configurations and code to set up and deploy a service using Kubernetes, Envoy, and Go. The service involves routing based on HTTP headers, collecting metrics, and ensuring efficient load balancing.

## Files

### Kubernetes Manifests

1. **gateway.yaml**
   - Configuration file to create Envoy GatewayClass, Envoy Gateway and bootstrap the Envoy Gateway.
   - **Detailed Description:**
     - The bootstrap config custom-proxy-config configures Envoy to listen on port 8081 and sets up HTTP filters for external processing (`ext_proc`) and original destination load balancing.
     - The listener on port 8081 is configured to handle incoming HTTP requests and route them based on custom logic.
     - The [External Processing (ext_proc)](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/ext_proc/v3/ext_proc.proto.html) filter is used for external processing of HTTP requests, allowing for custom request handling and routing decisions.
     - The [Original Destination Cluster](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/load_balancing/original_dst) is used to route traffic to the original destination specified in the `target-pod` header.
     - This configuration ensures that requests are processed and routed efficiently based on custom logic defined in the external processing service.

2. **gatewaysvc.patch**
   - A sample command to patch the Envoy Gateway service, adding a new port 8081.

3. **ext_proc.yaml**
   - External processing configuration for Envoy. Handles custom routing logic and processing of requests based on specific headers.
   - **Important:** Update this file with the pod names and internal IP addresses of the vLLM replicas.
   - **Important:** Update this file with the image created using the Dockerfile.

4. **vllm-lora-service.yaml**
   - Service configuration for the vLLM LoRa (Low-Rank Adaptation) service. Exposes the service within the Kubernetes cluster.

5. **vllm-lora-deployment.yaml**
   - Deployment configuration for the vLLM LoRa service. Defines how the service is deployed, including replicas, containers, and other deployment details.
   - **Note:** This deployment uses a custom image from [vLLM LoRa](https://github.com/kaushikmitr/vllm/tree/lora).

### Go and Docker Files (located in `ext_proc` folder)

1. **main.go**
   - Main Go application file. Contains the logic for handling HTTP headers and metrics. Includes functionality to save the latest response header in memory.

2. **go.mod**
   - Go module file. Specifies the module path and dependencies required for the Go application.

3. **go.sum**
   - Go checksum file. Ensures the integrity of the dependencies used in the Go application.

4. **Dockerfile**
   - Dockerfile to build the Go application into a Docker image. Defines the steps to compile the Go code and create a containerized application.

## Setup and Deployment

### Prerequisites

- Kubernetes cluster
- Docker
- `kubectl` command-line tool
- Envoy proxy
- Go (for local development)

### Steps

1. **Build the Docker Image**
   ```bash
   cd ext_proc
   docker build -t your-docker-repo/your-image-name:tag .
   ```

2. **Push the Docker Image**
   ```bash
   docker push your-docker-repo/your-image-name:tag
   ```

3. **Apply Kubernetes Manifests**
   ```bash
   kubectl apply -f gateway.yaml
   kubectl apply -f ext_proc.yaml
   kubectl apply -f vllm-lora-service.yaml
   kubectl apply -f vllm-lora-deployment.yaml
   ```

4. **Patch the Gateway Service (Update based on actual service name)**
   ```bash
        kubectl patch svc envoy-default-inference-gateway-xxxxxxxx  -n envoy-gateway-system -p '{"spec": {"ports": [{"name": "http-8081", "port": 8081, "targetPort": 8081, "protocol": "TCP"}]}}'
   ```

5. **Update `ext_proc.yaml`**
   - Ensure the `ext_proc.yaml` is updated with the pod names and internal IP addresses of the vLLM replicas. This step is crucial for the correct routing of requests based on headers.
   - Ensure the `ext_proc.yaml` is updated with the image created using the Dockerfile.

### Monitoring and Metrics

- The Go application collects metrics and saves the latest response headers in memory.
- Ensure Envoy is configured to route based on the metrics collected from the `/metric` endpoint of different service pods.

## Contributing

1. Fork the repository.
2. Create a new branch.
3. Make your changes.
4. Open a pull request.

## License

This project is licensed under the MIT License.

---

Feel free to customize this README to better fit your specific project details and requirements.
