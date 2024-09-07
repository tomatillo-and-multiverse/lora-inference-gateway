package redispriorityqueue

import (
	"ext-proc/cache"
	"log"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

type RedisPriorityQueueManager struct {
	lruCacheLLMRequest *expirable.LRU[string, cache.LLMRequest]
	redisPriorityQueue *RedisPriorityQueue
}

func NewRedisPriorityQueueManager(
	lruCache *expirable.LRU[string, cache.LLMRequest],
	pq *RedisPriorityQueue,
) *RedisPriorityQueueManager {
	return &RedisPriorityQueueManager{
		lruCacheLLMRequest: lruCache,
		redisPriorityQueue: pq,
	}
}

func (m *RedisPriorityQueueManager) calculateSum(values []float64) float64 {
	var sum float64
	for _, val := range values {
		sum += val
	}
	return sum
}

func (m *RedisPriorityQueueManager) CreateItem(reqId string, podUseCaseMetricsMap map[string]*cache.PodUseCaseMetrics, use_case string, priority int, weight float64) *Item {
	llmrequest, ok := cache.GetLRUCacheLLMRequest(m.lruCacheLLMRequest, reqId)
	if !ok {
		log.Printf("Error getting LLMRequest from cache for reqId: %s", reqId)
		return nil
	}

	all_prefill_latency_in_sec_per_token := []float64{}
	all_decode_latency_in_sec_per_token := []float64{}

	for key, metric := range podUseCaseMetricsMap {
		if key != use_case+":"+metric.PodName {
			continue
		}
		all_prefill_latency_in_sec_per_token = append(all_prefill_latency_in_sec_per_token, metric.DecodeLatenciesInSecPerToken...)
		all_decode_latency_in_sec_per_token = append(all_decode_latency_in_sec_per_token, metric.DecodeLatenciesInSecPerToken...)
	}

	avg_prefill_latency_in_sec_per_token := 0.0
	avg_decode_latency_in_sec_per_token := 0.0
	if len(all_prefill_latency_in_sec_per_token) > 0 {
		avg_prefill_latency_in_sec_per_token = m.calculateSum(all_prefill_latency_in_sec_per_token) / float64(len(all_prefill_latency_in_sec_per_token))
	}
	if len(all_decode_latency_in_sec_per_token) > 0 {
		avg_decode_latency_in_sec_per_token = m.calculateSum(all_decode_latency_in_sec_per_token) / float64(len(all_decode_latency_in_sec_per_token))
	}

	TTFT := avg_prefill_latency_in_sec_per_token * float64(llmrequest.TokensSent)
	TPOT := avg_decode_latency_in_sec_per_token

	if weight == 0 {
		weight = TTFT + TPOT*float64(llmrequest.TokensPending-llmrequest.TokensSent)
	}

	item := &Item{
		ID:           llmrequest.ReqID,
		Priority:     priority,
		Weight:       weight,
		ArrivalTime:  float64(time.Now().UnixNano()) / float64(time.Second),
		InputTokens:  llmrequest.TokensSent,
		OutputTokens: llmrequest.TokensPending - llmrequest.TokensSent,
		TTFT:         TTFT,
		TPOT:         TPOT,
		UseCase:      llmrequest.UseCase,
	}
	return item
}

func (m *RedisPriorityQueueManager) AddItemToQueue(item *Item) error {
	err := m.redisPriorityQueue.Push(item)
	if err != nil {
		log.Printf("Error pushing item to the queue: %v", err)
		return err
	}
	return nil
}

func (m *RedisPriorityQueueManager) WaitForTopItem(item *Item) (bool, error) {
	total_wait_time := 0
	for {
		if total_wait_time > 300000 {
			log.Printf("Timeout reached while waiting for the item to reach the top of the queue")
			return false, nil
		}
		topItem, err := m.redisPriorityQueue.Peek()
		if err != nil {
			log.Printf("Error getting top item from the queue: %v", err)
			return false, err
		}
		if topItem.ID == item.ID {
			return true, nil
		}
		time.Sleep(1 * time.Millisecond)
		total_wait_time++
	}
}
