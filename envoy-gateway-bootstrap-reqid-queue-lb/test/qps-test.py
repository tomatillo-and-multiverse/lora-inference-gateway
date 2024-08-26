
import asyncio
import random
import aiohttp
import time
import numpy as np

models = ["meta-llama/Llama-2-7b-hf"]

def load_test_prompts_short_math():
    return [("Describe in detail the philosophy of Schopenhauer", 20)]

def load_test_prompts_pure_dove():
    return [("Describe in detail the philosophy of Schopenhauer", 1024)]

def generate_request(prompt, output_token_count, model):
    """Generates request for given model server"""
    pload = {
        "prompt": prompt,
        "model": model,
        "max_tokens": output_token_count,
    }
    return pload

def get_target_pods(prompt_type=None):
    available_ips = ["10.56.2.37:8000","10.56.1.32:8000","10.56.3.46:8000" ]
    return random.choice(available_ips)

async def _send_chat_completion(session, prompt, url, result_queue, model, prompt_type, output_token_count, max_retries=1, randomize_target_pod=False):
    for attempt in range(max_retries):
        req_start_time = time.perf_counter()
        try:
            headers = {"Content-Type": "application/json"}
            target_pod = None
            if randomize_target_pod:
                target_pod = get_target_pods()
                headers.update({"target-pod": target_pod})
            if output_token_count is None:
                output_token_count = 20 if random.uniform(0, 1) < 0.5 else 1024
                prompt_type = 'short' if output_token_count < 100 else 'long'
            async with session.post(url=url,
                                    json=generate_request(prompt, output_token_count, model),
                                    headers=headers) as response:
                response_json = await response.json()
                req_end_time = time.perf_counter()
                latency = int(response.headers.get("x-envoy-upstream-service-time", -1))
                tokens_received = int(response_json['usage']['completion_tokens'])
                if target_pod is None:
                    target_pod = response.headers.get('target-pod', 'N/A')
                await result_queue.put((response_json, latency, req_start_time, req_end_time, prompt, prompt_type, model, tokens_received, target_pod))
                return
        except Exception as e:
            if attempt == max_retries - 1:
                req_end_time = time.perf_counter()
                latency = -1
                await result_queue.put((None, latency, req_start_time, req_end_time, prompt, prompt_type, model, 0, target_pod))

async def producer(no_of_messages, rate_per_second, session, result_queue, output_token_count=None, randomize_target_pod=False):
    url = "http://34.126.139.141:8081/v1/completions"
    
    tasks = []
    for _ in range(no_of_messages):
        prompt = """A1E: Deploy a second production workload on shared capacity
Problem: Buoyed by the success of the Gateway, the customer believes they are ready to integrate a second new workload and leverage their existing automation. The reduced cost to run duplicate copies of the model has allowed them to procure a smaller number of H100’s than they would have previously anticipated, and the increased velocity and automation also makes them more willing to experiment. They begin experimenting with the second workload by adding 4 H100’s directly to the shared pool thanks to the confidence they have in the latency objective guardrails.  The platform team wants to be certain they understand how two production workloads can share capacity without impacting the other before going live.  They also want to avoid the need to update each client to leverage the new adapter version, and instead orchestrate the rollout via their CD pipeline.

Action: They spawn a second CD pipeline to promote versions of the new fine tuned use case “summarize” as individual LLMRoutes. Instead of directly embedding the model name pointing to a specific adapter version in the clients, they define a new abstract model name “summarize_stage_latest” that the new clients will reference and have the automation update an HTTPRoute so that once an adapter passes individual qualification it replaces the latest adapter.

To verify that they can safely share capacity, they write a new performance test using a synthetic workload captured from the gateway’s traffic recording feature that can be started from the CI infrastructure against “summarize_stage_latest”.  They configure the LLMRoute to have a latency objective, and then begin very carefully ramping up the scale of the performance test.

Outcome: The platform team’s tests show that as traffic ramps up to summarize_stage_latest, it is automatically given a fair share of the overall capacity in terms of compute, but causes preproduction traffic to be excluded.  As they begin pushing load equivalent to 5 fully loaded accelerators on the new LLMRoute, they see alerts fire as the p95 TPOT latency for their production workload increases slightly.  Knowing the new use case is similar in input/output distribution to the original use case, fairly sharing capacity once the accelerators are saturated seems reasonable and the platform team gives the go ahead to rollout.
A1F: Detect and mitigate latency spikes due to KV-cache eviction
Problem: A few days after the first real client traffic began flowing to the summarization endpoint in production, an alert is raised that the latency objective is spiking several times over expected TPOT for the “safety” LLMRoute as well as an alert that KV-cache evictions are occurring.  The platform team must quickly determine the cause of the failure on the shared infrastructure.

Action: The alert for KV-cache evictions suggests longer responses may be causing GPU memory to fill up, leading vLLM to have to “evict” some requests and restart them.  Drilling into the capacity dashboard, they observe that evictions and GPU memory are indeed correlated in time, and they see a suggestion to view the p9x request lengths for all traffic flowing through the gateway for both use cases.  They observe that the longer response distributions are actually from their “summarize” use case - at certain periods during the day the output lengths of that use case are much longer than expected.

Using the gateway traffic recording feature they dig through into GCP Log Explorer to see which requests to the “summarize” endpoint have excessively long responses.  They then add those to their synthetic benchmark test and attempt to verify that they can trigger evictions.  Running the benchmark reproduces the spikes.

The user guide linked from the alert recommends an advanced configuration parameter for the LLMRoute that controls how the Gateway estimates the amount of tokens a given use case may generate.  Until the team can identify why the model generates excessively long summarizations, they can tell the Gateway to be more conservative in predicting the output length from the average length.  They also update the summarization client to set a stricter response length.

Outcome: The platform team received built in alerts, dashboards that quickly found correlations in key operational metrics, and was able to identify and reproduce the failure by observing traffic logs from the gateway.  Once they updated their LLMRoute to hint at longer requests, they saw fewer evictions while they worked to improve their model’s responses.
A1G: Prioritize use cases proportional to the
        """
        prompt_type = ""
        model = random.choice(models)
        task = _send_chat_completion(session, prompt, url, result_queue, model, prompt_type, output_token_count, 1, randomize_target_pod=randomize_target_pod)
        tasks.append(task)
        await asyncio.sleep(1 / rate_per_second)  # Maintain the rate of requests per second

    # Wait for all tasks to complete
    await asyncio.gather(*tasks)

async def consumer(result_queue, no_of_messages):
    responses = []
    latencies = []
    req_start_times = []
    req_end_times = []
    prompts = []
    tokens_received_list = []
    successful_requests = 0
    
    model_latencies = {model: [] for model in models}
    model_requests = {model: 0 for model in models}
    pod_latencies = {}
    pod_tokens = {}
    pod_start_times = {}
    pod_end_times = {}
    
    for _ in range(no_of_messages):
        response_json, latency, req_start_time, req_end_time, prompt, prompt_type, model, tokens_received, target_pod = await result_queue.get()
        if latency != -1:
            successful_requests += 1
            latencies.append(latency)
            responses.append(response_json)
            req_start_times.append(req_start_time)
            req_end_times.append(req_end_time)
            prompts.append(prompt)
            tokens_received_list.append(tokens_received)
            model_latencies[model].append(latency)
            model_requests[model] += 1
            
            if target_pod:
                if target_pod not in pod_latencies:
                    pod_latencies[target_pod] = []
                    pod_tokens[target_pod] = 0
                    pod_start_times[target_pod] = req_start_time
                    pod_end_times[target_pod] = req_end_time
                pod_latencies[target_pod].append(latency)
                pod_tokens[target_pod] += tokens_received
                pod_end_times[target_pod] = max(pod_end_times[target_pod], req_end_time)
    
    if successful_requests > 0:
        print(f"Overall Median latency: {np.percentile(latencies, 50):.4f} seconds over {successful_requests} requests")
        print(f"Overall 95th perc latency: {np.percentile(latencies, 95):.4f} seconds over {successful_requests} requests")
        
        total_tokens = sum(tokens_received_list)
        total_time = max(req_end_times) - min(req_start_times)
        tokens_per_second = total_tokens / total_time
        print(f"Tokens received per second: {tokens_per_second:.2f}")
        
        for model in models:
            if model_requests[model] > 0:
                print(f"Median latency for {model}: {np.percentile(model_latencies[model], 50):.4f} seconds over {model_requests[model]} requests")
                print(f"95th perc latency for {model}: {np.percentile(model_latencies[model], 95):.4f} seconds over {model_requests[model]} requests")
        
        for pod, latencies in pod_latencies.items():
            pod_total_time = pod_end_times[pod] - pod_start_times[pod]
            pod_tokens_per_second = pod_tokens[pod] / pod_total_time if pod_total_time > 0 else 0
            print(f"Median latency for pod {pod}: {np.percentile(latencies, 50):.4f} seconds over {len(latencies)} requests")
            print(f"95th perc latency for pod {pod}: {np.percentile(latencies, 95):.4f} seconds over {len(latencies)} requests")
            print(f"Total tokens processed by pod {pod}: {pod_tokens[pod]}")
            print(f"Tokens received per second for pod {pod}: {pod_tokens_per_second:.2f}")
    
    return responses, latencies, req_start_times, req_end_times, prompts, successful_requests

async def main():

    model_name = "meta-llama/Llama-2-7b-hf"
    duration = 30

    for rate_per_second in [1, 5, 10]:
        no_of_messages = duration * rate_per_second
        for randomize_target_pod in [False, True]:
            print(f"Running with rate_per_second={rate_per_second}, randomize_target_pod={randomize_target_pod}")
            output_token_count = 1024
            result_queue = asyncio.Queue()
            async with aiohttp.ClientSession() as session:
                producer_task = asyncio.create_task(
                    producer(no_of_messages, rate_per_second, session, result_queue, output_token_count, randomize_target_pod)
                )
                consumer_task = asyncio.create_task(consumer(result_queue, no_of_messages))
                await asyncio.gather(producer_task, consumer_task)
            print(f"Completed run with no_of_messages={no_of_messages}, randomize_target_pod={randomize_target_pod}\n")

if __name__ == '__main__':
    asyncio.run(main())
