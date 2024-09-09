import argparse
import asyncio
from dataclasses import dataclass, field
import json
import random
import time
from typing import AsyncGenerator, Dict, List, Tuple

import aiohttp
import numpy as np
from transformers import AutoTokenizer
from transformers import PreTrainedTokenizerBase

# (prompt len, output len, latency)
REQUEST_LATENCY: List[Tuple[int, int, float]] = []

MIN_SEQ_LEN = 4
CLIENT_TIMEOUT_SEC = 3 * 60 * 60
NEW_TEXT_KEY = "\nOutput:\n"

@dataclass
class PodMetrics:
    latencies: List[float] = field(default_factory=list)
    gpu_usages: List[float] = field(default_factory=list)
    run_queue_sizes: List[int] = field(default_factory=list)
    wait_queue_sizes: List[int] = field(default_factory=list)
    number_of_succesful_requests: int = 0
    number_of_failed_requests: int = 0
    total_prompt_tokens: int = 0
    total_completion_tokens: int = 0
    sent_timestamps: List[float] = field(default_factory=list)
    received_timestamps: List[float] = field(default_factory=list)
    status_code_counts: Dict[int, int] = field(default_factory=lambda: {200: 0, 408: 0, 503: 0, 429: 0, 504: 0}) # Initialize with common HTTP codes

# Dictionary to store QPS for different use cases
USE_CASE_QPS: Dict[str, float] = {
    "high-priority": 40,
    "use-case-1": 40,
}

QPS = 120

# Initialize a dictionary to store metrics per use case
use_case_metrics: Dict[str, Dict[str, PodMetrics]] = {
    use_case: {} for use_case in USE_CASE_QPS.keys()
}


def compute_pod_metrics(pod_name: str, metrics: PodMetrics, benchmark_time: float):
    if metrics.latencies:
        avg_latency = np.mean(metrics.latencies)
        p99_latency = np.percentile(metrics.latencies, 99)
    else:
        avg_latency, p99_latency = 0, 0

    if metrics.gpu_usages:
        avg_gpu_utilization = np.mean(metrics.gpu_usages)
        p99_gpu_utilization = np.percentile(metrics.gpu_usages, 99)
    else:
        avg_gpu_utilization, p99_gpu_utilization = 0, 0

    if metrics.run_queue_sizes:
        avg_run_queue_size = np.mean(metrics.run_queue_sizes)
        p99_run_queue_size = np.percentile(metrics.run_queue_sizes, 99)
    else:
        avg_run_queue_size, p99_run_queue_size = 0, 0

    if metrics.wait_queue_sizes:
        avg_wait_queue_size = np.mean(metrics.wait_queue_sizes)
        p99_wait_queue_size = np.percentile(metrics.wait_queue_sizes, 99)
    else:
        avg_wait_queue_size, p99_wait_queue_size = 0, 0

    total_output_tokens = metrics.total_completion_tokens
    output_tokens_per_min = 60 * total_output_tokens / benchmark_time if benchmark_time > 0 else 0

    total_input_tokens = metrics.total_prompt_tokens
    input_tokens_per_min = 60 * total_input_tokens / benchmark_time if benchmark_time > 0 else 0

    total_tokens = total_input_tokens + total_output_tokens
    tokens_per_min = 60 * total_tokens / benchmark_time if benchmark_time > 0 else 0
    
    print(f"\nMetrics for Pod: {pod_name}")
    print(f"Avg Latency: {avg_latency:.2f} seconds")
    print(f"99th Percentile Latency: {p99_latency:.2f} seconds")
    print(f"Avg GPU Utilization: {avg_gpu_utilization:.2f}%")
    print(f"99th Percentile GPU Utilization: {p99_gpu_utilization:.2f}%")
    print(f"Avg Running Queue Size: {avg_run_queue_size:.2f}")
    print(f"99th Percentile Running Queue Size: {p99_run_queue_size:.2f}")
    print(f"Avg Waiting Queue Size: {avg_wait_queue_size:.2f}")
    print(f"99th Percentile Waiting Queue Size: {p99_wait_queue_size:.2f}")
    print(f"Total Successful Requests: {metrics.number_of_succesful_requests}")
    print(f"Total Failed Requests: {metrics.number_of_failed_requests}")
    print(f"Output_tokens/min: {output_tokens_per_min:.2f}")
    print(f"Input_tokens/min: {input_tokens_per_min:.2f}")
    print(f"Tokens/min: {tokens_per_min:.2f}")

    # Print the status code counts
    print("Status Code Counts:")
    for code, count in metrics.status_code_counts.items():
        print(f"  {code}: {count}")
        
def compute_overall_use_case_metrics(benchmark_time: float):
    for use_case, metrics_dict in use_case_metrics.items():
        total_latencies = []
        total_gpu_usages = []
        total_run_queue_sizes = []
        total_wait_queue_sizes = []
        total_successful_requests = 0
        total_failed_requests = 0
        total_prompt_tokens = 0
        total_completion_tokens = 0
        overall_status_code_counts: Dict[int, int] = {}

        for pod_name, metrics in metrics_dict.items():
            print(f"\n--- Pod-specific Metrics for Use Case: {use_case} ---")
            compute_pod_metrics(pod_name, metrics, benchmark_time)

            total_latencies.extend(metrics.latencies)
            total_gpu_usages.extend(metrics.gpu_usages)
            total_run_queue_sizes.extend(metrics.run_queue_sizes)
            total_wait_queue_sizes.extend(metrics.wait_queue_sizes)
            total_successful_requests += metrics.number_of_succesful_requests
            total_failed_requests += metrics.number_of_failed_requests
            total_prompt_tokens += metrics.total_prompt_tokens
            total_completion_tokens += metrics.total_completion_tokens

            # Aggregate status code counts across all pods for the use case
            for code, count in metrics.status_code_counts.items():
                if code in overall_status_code_counts:
                    overall_status_code_counts[code] += count
                else:
                    overall_status_code_counts[code] = count


            min_sent_timestamps = np.min(metrics.sent_timestamps) if len(metrics.sent_timestamps) > 0 else 0
            max_received_timestamps = np.max(metrics.received_timestamps) if len(metrics.received_timestamps) > 0 else 0
            request_received_per_sec  = (max_received_timestamps-min_sent_timestamps)/metrics.number_of_succesful_requests if metrics.number_of_succesful_requests > 0 else 0

        if total_latencies:
            avg_latency = np.mean(total_latencies)
            p99_latency = np.percentile(total_latencies, 99)
        else:
            avg_latency, p99_latency = 0, 0

        if total_gpu_usages:
            avg_gpu_utilization = np.mean(total_gpu_usages)
            p99_gpu_utilization = np.percentile(total_gpu_usages, 99)
        else:
            avg_gpu_utilization, p99_gpu_utilization = 0, 0

        if total_run_queue_sizes:
            avg_run_queue_size = np.mean(total_run_queue_sizes)
            p99_run_queue_size = np.percentile(total_run_queue_sizes, 99)
        else:
            avg_run_queue_size, p99_run_queue_size = 0, 0

        if total_wait_queue_sizes:
            avg_wait_queue_size = np.mean(total_wait_queue_sizes)
            p99_wait_queue_size = np.percentile(total_wait_queue_sizes, 99)
        else:
            avg_wait_queue_size, p99_wait_queue_size = 0, 0

        total_output_tokens = total_completion_tokens
        output_tokens_per_min = 60 * total_output_tokens / benchmark_time if benchmark_time > 0 else 0

        total_input_tokens = total_prompt_tokens
        input_tokens_per_min = 60 * total_input_tokens / benchmark_time if benchmark_time > 0 else 0

        total_tokens = total_input_tokens + total_output_tokens
        tokens_per_min = 60 * total_tokens / benchmark_time if benchmark_time > 0 else 0

        print(f"\nOverall Metrics for Use Case: {use_case}")
        print(f"Avg Latency: {avg_latency:.2f} seconds")
        print(f"99th Percentile Latency: {p99_latency:.2f} seconds")
        print(f"Avg GPU Utilization: {avg_gpu_utilization:.2f}%")
        print(f"99th Percentile GPU Utilization: {p99_gpu_utilization:.2f}%")
        print(f"Avg Running Queue Size: {avg_run_queue_size:.2f}")
        print(f"99th Percentile Running Queue Size: {p99_run_queue_size:.2f}")
        print(f"Avg Waiting Queue Size: {avg_wait_queue_size:.2f}")
        print(f"99th Percentile Waiting Queue Size: {p99_wait_queue_size:.2f}")
        print(f"Total Successful Requests: {total_successful_requests}")
        print(f"Total Failed Requests: {total_failed_requests}")
        print(f"Output_tokens/min: {output_tokens_per_min:.2f}")
        print(f"Input_tokens/min: {input_tokens_per_min:.2f}")
        print(f"Tokens/min: {tokens_per_min:.2f}")
        print(f"Request Received/sec: {request_received_per_sec:.2f}")

        # Print the overall status code counts for the use case
        print("Overall Status Code Counts:")
        for code, count in overall_status_code_counts.items():
            print(f"  {code}: {count}")




async def periodic_print_stats(benchmark_start_time: float):
    """Periodically print stats every 2 minutes."""
    while True:
        await asyncio.sleep(120)  # Sleep for 2 minutes
        benchmark_time = time.time() - benchmark_start_time
        compute_overall_use_case_metrics(benchmark_time)

def sample_requests(
    dataset_path: str,
    num_requests: int,
    max_input_len: int,
    max_output_len: int,
    tokenizer: PreTrainedTokenizerBase,
    use_dummy_text: bool,
) -> List[Tuple[str, int, int, str]]:
    """Samples requests from the dataset or creates dummy requests."""
    if use_dummy_text:
        use_case = "basemodel"
        dummy_prompt_token_ids = [0] * max_input_len
        dummy_prompt = tokenizer.decode(dummy_prompt_token_ids)
        dummy_requests = [(
            dummy_prompt,
            max_input_len,
            max_output_len, use_case
        )] * num_requests
        return dummy_requests

    # Load the dataset.
    with open(dataset_path) as f:
        dataset = json.load(f)
    dataset = [data for data in dataset if len(data["conversations"]) >= 2]
    dataset = [
        (data["conversations"][0]["value"], data["conversations"][1]["value"])
        for data in dataset
    ]

    # Tokenize the prompts and completions.
    prompts = [prompt for prompt, _ in dataset]
    prompt_token_ids = tokenizer(prompts).input_ids
    completions = [completion for _, completion in dataset]
    completion_token_ids = tokenizer(completions).input_ids
    tokenized_dataset = []
    for i in range(len(dataset)):
        output_len = len(completion_token_ids[i])
        tokenized_dataset.append((prompts[i], prompt_token_ids[i], output_len))

    # Filter out too long sequences.
    filtered_dataset: List[Tuple[str, int, int, str]] = []
    for prompt, prompt_token_ids, output_len in tokenized_dataset:
        use_case = random.choice(list(USE_CASE_QPS.keys()))
        prompt_len = len(prompt_token_ids)
        if prompt_len < MIN_SEQ_LEN or output_len < MIN_SEQ_LEN:
            continue
        if prompt_len > max_input_len or output_len > max_output_len:
            continue
        filtered_dataset.append((prompt, prompt_len, output_len, use_case))

    # Sample the requests.
    sampled_requests = random.sample(filtered_dataset, num_requests)
    return sampled_requests

async def get_request(
    input_requests: List[Tuple[str, int, int, str]],
    request_rate: float = float("inf"),
) -> AsyncGenerator[Tuple[str, int, int, str], None]:
    """Gets request async for a specific use case."""
    input_requests = iter(input_requests)
    for request in input_requests:
        yield request
        request_rate = request_rate
        if request_rate == float("inf"):
            continue
        interval = 1 / request_rate
        await asyncio.sleep(interval)

def get_target_pods():
    available_ips = ["10.56.3.88:8000", "10.56.1.36:8000", "10.56.2.85:8000"]
    return random.choice(available_ips)

# Update `send_request` to track the status code
async def send_request(
    backend: str,
    api_url: str,
    prompt: str,
    prompt_len: int,
    output_len: int,
    best_of: int,
    use_beam_search: bool,
    top_k: int,
    tokenizer: PreTrainedTokenizerBase,
    sax_model: str,
    use_case: str,
    random_pod: bool
) -> None:
    """Sends request to server."""
    request_start_time = time.time()
    headers = {"Content-Type": "application/json"}
    target_pod = ""
    print(f"sending usecase: {use_case}")
    if random_pod:
        target_pod = get_target_pods()
        headers["target-pod"] = target_pod
    if backend == "vllm":
        pload = {
            "prompt": prompt,
            "temperature": 0.0 if use_beam_search else 1.0,
            "max_tokens": output_len,
            "prompt_len": prompt_len,
            "use_case": use_case,
            "model": "meta-llama/Llama-2-7b-hf",
        }
    else:
        raise ValueError(f"Unknown backend: {backend}")

    timeout = aiohttp.ClientTimeout(total=CLIENT_TIMEOUT_SEC)
    error_flag = False
    status_code = 0
    async with aiohttp.ClientSession(timeout=timeout) as session:
        while True:
            try:
                async with session.post(api_url, headers=headers, json=pload ) as response:
                    status_code = response.status
                    if status_code != 200:
                        error_flag = True
                        break
                    chunks = []
                    async for chunk, _ in response.content.iter_chunks():
                        chunks.append(chunk)
                    output = b"".join(chunks).decode("utf-8")
                    output = json.loads(output)
                    headers = response.headers
            except Exception:
                error_flag = True
                status_code = 500  # Assume server error if an exception occurs
                break
            break

    request_end_time = time.time()
    if use_case not in use_case_metrics:
        use_case_metrics[use_case] = {}
    pod_name = headers.get('target-pod', target_pod)
    if pod_name not in use_case_metrics[use_case]:
        use_case_metrics[use_case][pod_name] = PodMetrics()

    pod_metrics = use_case_metrics[use_case][pod_name]

    # Update status code counts
    if status_code in pod_metrics.status_code_counts:
        pod_metrics.status_code_counts[status_code] += 1
    else:
        pod_metrics.status_code_counts[status_code] = 1

    if backend == "vllm" and not error_flag:
        prompt_len = int(output["usage"]["prompt_tokens"])
        output_len = int(output["usage"]["completion_tokens"])
        

        gpu_usage = float(headers['gpu_cache_usage_sys']) * 100
        running_queue_size = int(headers['running_queue_size'])
        waiting_queue_size = int(headers['waiting_queue_size'])
        
        request_latency = float(request_end_time-request_start_time)
        REQUEST_LATENCY.append((prompt_len, output_len, request_latency))

        pod_metrics.latencies.append(request_latency)
        pod_metrics.gpu_usages.append(gpu_usage)
        pod_metrics.run_queue_sizes.append(running_queue_size)
        pod_metrics.wait_queue_sizes.append(waiting_queue_size)
        pod_metrics.total_prompt_tokens += prompt_len
        pod_metrics.total_completion_tokens += output_len
        pod_metrics.number_of_succesful_requests += 1
        pod_metrics.sent_timestamps.append(request_start_time)
        pod_metrics.received_timestamps.append(request_end_time)
    else:
        pod_metrics.number_of_failed_requests += 1

    use_case_metrics[use_case][pod_name] = pod_metrics

async def benchmark(
    backend: str,
    api_url: str,
    input_requests: List[Tuple[str, int, int, str]],
    best_of: int,
    use_beam_search: bool,
    request_rate: float,
    top_k: int,
    tokenizer: PreTrainedTokenizerBase,
    sax_model: str,
    random_pod: bool,
    benchmark_start_time: float
) -> None:
    """Runs benchmark with asynchronous requests for each use case."""
    tasks: List[asyncio.Task] = []
    #stats_task = asyncio.create_task(periodic_print_stats(benchmark_start_time))  # Start periodic stats printing
    async for request in get_request(input_requests, request_rate):
        prompt, prompt_len, output_len, use_case = request
        task = asyncio.create_task(
            send_request(
                backend,
                api_url,
                prompt,
                prompt_len,
                output_len,
                best_of,
                use_beam_search,
                top_k,
                tokenizer,
                sax_model,
                use_case,
                random_pod
            )
        )
        tasks.append(task)
    await asyncio.gather(*tasks)

def main(args: argparse.Namespace):
    print(args)
    random.seed(args.seed)
    np.random.seed(args.seed)

    api_url = f"http://35.240.215.93:8081/v1/completions/"
    tokenizer = AutoTokenizer.from_pretrained(
        args.tokenizer, trust_remote_code=args.trust_remote_code
    )
    input_requests = sample_requests(
        args.dataset,
        args.num_prompts,
        args.max_input_length,
        args.max_output_length,
        tokenizer,
        args.use_dummy_text
    )

    benchmark_start_time = time.time()
    asyncio.run(
        benchmark(
            args.backend,
            api_url,
            input_requests,
            args.best_of,
            args.use_beam_search,
            args.request_rate,
            args.top_k,
            tokenizer,
            args.sax_model,
            args.random_pod,
            benchmark_start_time
        )
    )
    benchmark_end_time = time.time()
    benchmark_time = benchmark_end_time - benchmark_start_time
    print(f"Total time: {benchmark_time:.2f} s")
    print(f"Total successful requests: {len(REQUEST_LATENCY)}")
    print(f"Requests/min: {60 * len(REQUEST_LATENCY) / benchmark_time:.2f}")

    total_output_tokens = np.sum([output_len for _, output_len, _ in REQUEST_LATENCY])
    output_tokens_per_min = 60 * total_output_tokens / benchmark_time
    print(f"Output_tokens/min: {output_tokens_per_min:.2f}")

    total_input_tokens = np.sum([prompt_len for prompt_len, _, _ in REQUEST_LATENCY])
    input_tokens_per_min = 60 * total_input_tokens / benchmark_time
    print(f"Input_tokens/min: {input_tokens_per_min:.2f}")

    total_tokens = total_input_tokens + total_output_tokens
    tokens_per_min = 60 * total_tokens / benchmark_time
    print(f"Tokens/min: {tokens_per_min:.2f}")

    if args.machine_cost:
        print(
            "Cost $/1k tokens:"
            f" {args.machine_cost * 1000 / (60 * output_tokens_per_min)}"
        )
    avg_latency = np.mean([latency for _, _, latency in REQUEST_LATENCY])
    print(
        "Average seconds/request (includes waiting time on server):"
        f" {avg_latency:.2f}"
    )

    avg_per_token_latency = np.mean([
        latency / (prompt_len + output_len)
        for prompt_len, output_len, latency in REQUEST_LATENCY
    ])
    print(
        "Average milliseconds/token (includes waiting time on server):"
        f" {1000 * avg_per_token_latency:.2f}"
    )

    avg_per_output_token_latency = np.mean(
        [latency / output_len for _, output_len, latency in REQUEST_LATENCY]
    )
    print(
        "Average milliseconds/output_token (includes waiting time on server):"
        f" {1000 * avg_per_output_token_latency:.2f}"
    )

    avg_input_len = np.mean(
        [prompt_len for prompt_len, _, _ in REQUEST_LATENCY]
    )
    print(
        "Average input length:"
        f" {avg_input_len:.2f}"
    )

    avg_output_len = np.mean(
        [output_len for _, output_len, _ in REQUEST_LATENCY]
    )
    print(
        "Average output length:"
        f" {avg_output_len:.2f}"
    )

    compute_overall_use_case_metrics(benchmark_time)

if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Benchmark the online serving throughput."
    )
    parser.add_argument(
        "--backend",
        type=str,
        default="vllm",
        choices=["vllm"],
    )
    parser.add_argument(
        "--sax_model",
        type=str,
        default="",
        help="Model name to send request to at API server for SAX model server.",
    )
    parser.add_argument("--endpoint", type=str, default="generate")
    parser.add_argument("--host", type=str, default="localhost")
    parser.add_argument("--port", type=int, default=7080)
    parser.add_argument("--dataset", type=str, help="Path to the dataset.")
    parser.add_argument(
        "--tokenizer",
        type=str,
        required=True,
        help="Name or path of the tokenizer.",
    )
    parser.add_argument(
        "--best-of",
        type=int,
        default=1,
        help="Generates `best_of` sequences per prompt and returns the best one.",
    )
    parser.add_argument("--use-beam-search", action="store_true")
    parser.add_argument(
        "--num-prompts",
        type=int,
        default=5,
        help="Number of prompts to process.",
    )
    parser.add_argument(
        "--max-input-length",
        type=int,
        default=1024,
        help="Maximum number of input tokens for filtering the benchmark dataset."
    )
    parser.add_argument(
        "--max-output-length",
        type=int,
        default=1024,
        help="Maximum number of input tokens for filtering the benchmark dataset."
    )
    parser.add_argument(
        "--top-k",
        type=int,
        default=32000,
        help=(
            "Number of candidate tokens that are considered at each step of the"
            " generation process. 32000 is the vocab_size of Open-LLaMA and"
            " LLaMA2 models."
        ),
    )
    parser.add_argument(
        "--request-rate",
        type=float,
        default=float("inf"),
        help=(
            "Number of requests per second. If this is inf, "
            "then all the requests are sent at time 0. "
            "Otherwise, we use Poisson process to synthesize "
            "the request arrival times."
        ),
    )
    parser.add_argument("--seed", type=int, default=0)
    parser.add_argument(
        "--trust-remote-code",
        action="store_true",
        help="trust remote code from huggingface",
    )
    parser.add_argument(
        "--machine-cost",
        type=float,
        default=None,
        help="Machine cost per hour including accelerators (if any)",
    )
    parser.add_argument(
        "--use-dummy-text",
        action="store_true",
        help=(
            "Whether to use dummy text with length defined by max_input_length"
            " and max_output_length."
        ),
    )
    parser.add_argument("--random-pod", type=bool, default=False)
    cmd_args = parser.parse_args()
    main(cmd_args)
