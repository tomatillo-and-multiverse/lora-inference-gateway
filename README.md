# Envoy Ext Proc Gateway with LoRA Integration

This project sets up an Envoy gateway with a custom external processing which  implements advanced routing logic tailored for LoRA (Low-Rank Adaptation) adapters. The routing algorithm is based on the model specified (using Open AI API format), and ensuring efficient load balancing based on model server metrics.

![alt text](./doc/envoy-gateway-bootstrap.png)

## Requirements
- A vLLM based deployment (using the custom image provided below), with LoRA Adapters.  ***This PoC uses a modified vLLM fork, the public image of the fork is here: `ghcr.io/tomatillo-and-multiverse/vllm:demo`***. A sample deployement is provided under `./manifests/samples/vllm`.
- Kubernetes cluster
- Envoy Gateway v1.1 installed on your cluster: https://gateway.envoyproxy.io/v1.1/tasks/quickstart/
- `kubectl` command-line tool
- Go (for local development)

## Quickstart

### Steps
1. **Install LLMRoute CRD**
   ```bash
   kubectl apply -f ./crd/config/crd/bases/
   ```

1. **Install GatewayClass**
   A custom GatewayClass `llm-gateway` which is configured with the llm routing ext proc will be installed into the `llm-gateway` namespace. When you create Gateways, make sure the `llm-gateway` GatewayClass is used.

   NOTE: Ensure the `llm-route-ext-proc` deployment is updated with the pod names and internal IP addresses of the vLLM replicas. This step is crucial for the correct routing of requests based on headers. This won't be needed once we make ext proc dynamically read the pods.

   ```bash
   kubectl apply -f ./manifests/gatewayclass.yaml
   ```
1. **Deploy Sample Application**
   
   ```bash
   kubectl apply -f ./manifests/vllm
   kubectl apply -f ./manifests/samples/gateway.yaml
   kubectl apply -f ./manifests/samples/llmroute.yaml
   ```

2. **Try it out** 
   ```bash
   IP=$(kubectl get gateway/llm-gateway -o jsonpath='{.status.addresses[0].value}')
   PORT=8081

   curl -i ${IP}:${PORT}/v1/completions -H 'Content-Type: application/json' -d '{
   "model": "tweet-summary",
   "prompt": "Write as if you were a critic: San Francisco",
   "max_tokens": 100,
   "temperature": 0
   }'
   ```

## Contributing

1. Fork the repository.
1. Make your changes.
1. Open a pull request.

## License

This project is licensed under the MIT License.

---

Feel free to customize this README to better fit your specific project details and requirements.
