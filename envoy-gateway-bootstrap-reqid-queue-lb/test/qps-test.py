r"""Benchmark LLM serving throughput and latency.
This script is for sending requests with prompts to LLM server and benchmark
the latency and throughput at various request rates. It is a modified version of
https://github.com/vllm-project/vllm/blob/main/benchmarks/benchmark_serving.py.
It currently supports TGI, vLLM, Triton TensorRT-LLM and Saxml.
"""

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

# Initialize a dictionary to store pod metrics
pod_details_dict: Dict[str, PodMetrics] = {}


def sample_requests(
    dataset_path: str,
    num_requests: int,
    max_input_len: int,
    max_output_len: int,
    tokenizer: PreTrainedTokenizerBase,
    use_dummy_text: bool,
) -> List[Tuple[str, int, int, str]]:
  """Samples requests from the dataset or creates dummy requests."""
  use_case = "basemodel"
  if use_dummy_text:
    dummy_prompt_token_ids = [0] * max_input_len
    dummy_prompt = tokenizer.decode(dummy_prompt_token_ids)
    dummy_requests = [(
        dummy_prompt,
        max_input_len,
        max_output_len,use_case
    )] * num_requests
    return dummy_requests

  # Load the dataset.
  with open(dataset_path) as f:
    dataset = json.load(f)
  # Filter out the conversations with less than 2 turns.
  dataset = [data for data in dataset if len(data["conversations"]) >= 2]
  # Only keep the first two turns of each conversation.
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
  filtered_dataset: List[Tuple[str, int, int]] = []
  for prompt, prompt_token_ids, output_len in tokenized_dataset:
    prompt_len = len(prompt_token_ids)
    if prompt_len < MIN_SEQ_LEN or output_len < MIN_SEQ_LEN:
      # Prune too short sequences.
      # This is because TGI causes errors when the input or output length
      # is too short.
      continue
    if prompt_len > max_input_len or output_len > max_output_len:
      # Prune too long sequences.
      continue
    filtered_dataset.append((prompt, prompt_len, output_len, use_case))

  # Sample the requests.
  sampled_requests = random.sample(filtered_dataset, num_requests)
  return sampled_requests


async def get_request(
    input_requests: List[Tuple[str, int, int]],
    request_rate: float,
) -> AsyncGenerator[Tuple[str, int, int], None]:
  """Gets request async."""
  input_requests = iter(input_requests)
  for request in input_requests:
    yield request

    if request_rate == float("inf"):
      # If the request rate is infinity, then we don't need to wait.
      continue
    # Sample the request interval from the exponential distribution.
    interval = np.random.exponential(1.0 / request_rate)
    # The next request will be sent after the interval.
    await asyncio.sleep(interval)

def get_target_pods():
    available_ips = ["10.56.3.88:8000","10.56.1.36:8000","10.56.2.85:8000"  ]
    return random.choice(available_ips)

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

  headers = { "Content-Type": "application/json",}
  target_pod = ""
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

  # Set client timeout to be 3 hrs.
  #print(f"Sending request to {api_url} with payload: {pload} and headers: {headers}")
  timeout = aiohttp.ClientTimeout(total=CLIENT_TIMEOUT_SEC)
  error_flag = False
  async with aiohttp.ClientSession(timeout=timeout) as session:
    while True:
        try:
            #print(f"Sending request to {api_url} with payload: {pload} and headers: {headers}")
            async with session.post(api_url, headers=headers, json=pload) as response:
                chunks = []
                async for chunk, _ in response.content.iter_chunks():
                    chunks.append(chunk)
                output = b"".join(chunks).decode("utf-8")
                output = json.loads(output)
                headers = response.headers
        except Exception as error:
          error_flag = True
          break
          #num_errors += 1
          #print(f"Error number {num_errors} decoding JSON response from server: {error}")
          #continue
          
          


        break

  #print(output)
  request_end_time = time.time()
  # Naive HF transformers generation and TensorRT-LLM generation stops at EOS
  # tokens and the generation may be shorter than the ground-truth output
  # sequence length.
  if backend == "vllm" and not error_flag: 

        prompt_len = int(output["usage"]["prompt_tokens"])
        output_len = int(output["usage"]["completion_tokens"])
        request_latency = float(output["usage"]["e2e_latency_in_sec"])
        REQUEST_LATENCY.append((prompt_len, output_len, request_latency))
        pod_name = headers.get('target-pod', target_pod)

        # Extract pod metrics
        gpu_usage = float(headers['gpu_cache_usage_sys'])*100
        running_queue_size = int(headers['running_queue_size'])
        waiting_queue_size = int(headers['waiting_queue_size'])

        # Update pod details dictionary
        if pod_name not in pod_details_dict:
            pod_details_dict[pod_name] = PodMetrics()

        pod_metrics = pod_details_dict[pod_name]
        pod_metrics.latencies.append(request_latency)
        pod_metrics.gpu_usages.append(gpu_usage)
        pod_metrics.run_queue_sizes.append(running_queue_size)
        pod_metrics.wait_queue_sizes.append(waiting_queue_size)
        pod_metrics.total_prompt_tokens += prompt_len
        pod_metrics.total_completion_tokens += output_len
        pod_metrics.number_of_succesful_requests    += 1
        pod_details_dict[pod_name] = pod_metrics
        

        

        #print(f'pod: {pod_name}, latency: {request_latency}, gpu_usage: {gpu_usage}, running_queue_size: {running_queue_size}, waiting_queue_size: {waiting_queue_size}')


async def benchmark(
    backend: str,
    api_url: str,
    input_requests: List[Tuple[str, int, int]],
    best_of: int,
    use_beam_search: bool,
    request_rate: float,
    top_k: int,
    tokenizer: PreTrainedTokenizerBase,
    sax_model: str,
    random_pod: bool
) -> None:
  """Runs benchmark with asynchronous requests."""
  tasks: List[asyncio.Task] = []
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

  #api_url = f"http://{args.host}:{args.port}/{args.endpoint}"
  api_url = f"http://34.126.139.141:8081/v1/completions/"
  tokenizer = AutoTokenizer.from_pretrained(
      args.tokenizer, trust_remote_code=args.trust_remote_code
  )
  input_requests = sample_requests(
      args.dataset,
      args.num_prompts,
      args.max_input_length,
      args.max_output_length,
      tokenizer,
      False,
        #args.use_dummy_text,
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
          args.random_pod
      )
  )
  benchmark_end_time = time.time()
  benchmark_time = benchmark_end_time - benchmark_start_time
  print(f"Total time: {benchmark_time:.2f} s")
  print(f"Total successful requests: {len(REQUEST_LATENCY)}")
  print(f"Requests/min: {60 * len(REQUEST_LATENCY) / benchmark_time:.2f}")

  total_output_tokens = np.sum([output_len for _, output_len, _ in
                                REQUEST_LATENCY])
  output_tokens_per_min = 60 * total_output_tokens / benchmark_time
  print(f"Output_tokens/min: {output_tokens_per_min:.2f}")

  total_input_tokens = np.sum([prompt_len for prompt_len, _, _ in
                               REQUEST_LATENCY])
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
  # NOTE: The latency below includes requests awaiting time on server side.
  # It's not comparable with the model inference latency for batch size 1.
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
  


  # Calculate and print pod statistics
  for pod_name, metrics in pod_details_dict.items():
        print(f"\nPod: {pod_name}")
        print(f"Avg Latency: {np.mean(metrics.latencies):.2f} seconds")
        print(f"99th Percentile Latency: {np.percentile(metrics.latencies, 99):.2f} seconds")

        print(f"Avg GPU Utilization: {np.mean(metrics.gpu_usages):.2f}%")
        print(f"99th Percentile GPU Utilization: {np.percentile(metrics.gpu_usages, 99):.2f}%")

        print(f"Avg Running Queue Size: {np.mean(metrics.run_queue_sizes):.2f}")
        print(f"99th Percentile Running Queue Size: {np.percentile(metrics.run_queue_sizes, 99):.2f}")

        print(f"Avg Waiting Queue Size: {np.mean(metrics.wait_queue_sizes):.2f}")
        print(f"99th Percentile Waiting Queue Size: {np.percentile(metrics.wait_queue_sizes, 99):.2f}")
        
        print(f"Number of successful requests: {metrics.number_of_succesful_requests}")

        
        total_output_tokens = metrics.total_completion_tokens
        output_tokens_per_min = 60 * total_output_tokens / benchmark_time
        print(f"Output_tokens/min: {output_tokens_per_min:.2f}")

        total_input_tokens = metrics.total_prompt_tokens
        input_tokens_per_min = 60 * total_input_tokens / benchmark_time
        print(f"Input_tokens/min: {input_tokens_per_min:.2f}")

        total_tokens = total_input_tokens + total_output_tokens
        tokens_per_min = 60 * total_tokens / benchmark_time
        print(f"Tokens/min: {tokens_per_min:.2f}")




if __name__ == "__main__":
  parser = argparse.ArgumentParser(
      description="Benchmark the online serving throughput."
  )
  parser.add_argument(
      "--backend",
      type=str,
      default="vllm",
      choices=[
          "vllm",
      ],
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
      help=(
          "Maximum number of input tokens for filtering the benchmark dataset."
      ),
  )
  parser.add_argument(
      "--max-output-length",
      type=int,
      default=1024,
      help=(
          "Maximum number of input tokens for filtering the benchmark dataset."
      ),
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
	