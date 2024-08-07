# Envoy Ext Proc Gateway with LoRA Integration

This project sets up an Envoy gateway to handle gRPC calls with integration of LoRA (Low-Rank Adaptation). The configuration aims to manage gRPC traffic through Envoy's external processing and custom routing based on headers and load balancing rules. The setup includes Kubernetes services and deployments for both the gRPC server and the vllm-lora application.

## Requirements
- Envoy Gateway v1.1 installed on your cluster: https://gateway.envoyproxy.io/v1.1/tasks/quickstart/

## vLLM
This PoC uses a modified vLLM fork, the public image of the fork is here: `ghcr.io/tomatillo-and-multiverse/vllm:demo`