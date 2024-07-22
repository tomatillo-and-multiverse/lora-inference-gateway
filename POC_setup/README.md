
# Envoy gRPC Gateway with LoRA Integration

This repository contains the configuration and deployment files for an Envoy gateway setup with gRPC services and LoRA integration. The setup aims to handle gRPC calls with Envoy's external processing and routing based on custom headers and load balancing.

## Overview

The project includes the following components:
1. **vllm-lora-service.yaml**: Kubernetes service for the vllm-lora application.
2. **vllm-lora-deployment.yaml**: Kubernetes deployment for the vllm-lora application.
3. **grpc_server_go.yaml**: Configuration for the gRPC server written in Go.
4. **main.go**: Go source file for the gRPC server implementation.
5. **httproute.yaml**: HTTPRoute configuration for routing traffic to the appropriate services.
6. **grpc_envoy_service.yaml**: Kubernetes service for the Envoy proxy.
7. **grpc_envoy_deployment.yaml**: Kubernetes deployment for the Envoy proxy.
8. **grpc_envoy_configmap.yaml**: ConfigMap for Envoy's configuration settings.
9. **gateway.yaml**: Gateway configuration for handling external traffic.
10. **Dockerfile**: Dockerfile for building the gRPC server application.

## Prerequisites

- Kubernetes Cluster
- Docker
- kubectl
- Envoy Proxy

## Setup

### 1. Build and Push Docker Image

Navigate to the directory containing the `Dockerfile` and build the Docker image:

```sh
docker build -t <your-docker-repo>/grpc-server:latest .
docker push <your-docker-repo>/grpc-server:latest
```

### 2. Deploy vllm-lora Application

Apply the service and deployment files for the vllm-lora application:

```sh
kubectl apply -f /mnt/data/vllm-lora-service.yaml
kubectl apply -f /mnt/data/vllm-lora-deployment.yaml
```

**Note:** The `vllm-lora-deployment.yaml` uses a custom vllm image specific to this setup. This (custom vLLM server)[https://github.com/kaushikmitr/vllm] emits LoRA metrics in the '/metrics' endpoint ad well in the response header.

### 3. Deploy gRPC Server

Apply the configuration and deployment files for the gRPC server:

```sh
kubectl apply -f /mnt/data/grpc_server_go.yaml
kubectl apply -f /mnt/data/grpc_envoy_service.yaml
kubectl apply -f /mnt/data/grpc_envoy_deployment.yaml
kubectl apply -f /mnt/data/grpc_envoy_configmap.yaml
```

### 4. Set Up Gateway and Routes

Configure the gateway and HTTP routes:

```sh
kubectl apply -f /mnt/data/gateway.yaml
kubectl apply -f /mnt/data/httproute.yaml
```

## Configuration Details

### Envoy Configuration

The `grpc_envoy_configmap.yaml` file contains the Envoy configuration for handling gRPC traffic, including external processing and custom routing logic based on headers and load balancing.

### gRPC Server

The gRPC server, implemented in Go (`main.go`), handles incoming requests and integrates with the Envoy proxy for custom processing.

## Usage

Once deployed, the Envoy gateway will route incoming gRPC requests to the appropriate services based on the custom headers and load balancing rules defined in the configuration.

## Contributing

Contributions are welcome! Please submit a pull request or open an issue to discuss any changes.

## License

This project is licensed under the MIT License.
