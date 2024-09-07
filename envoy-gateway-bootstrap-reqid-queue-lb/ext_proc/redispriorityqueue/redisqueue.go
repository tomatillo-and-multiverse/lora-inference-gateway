package redispriorityqueue

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

type Item struct {
	ID           string
	Priority     int
	Weight       float64
	ArrivalTime  float64 // Store the arrival time as Unix timestamp
	InputTokens  int     // Number of input tokens
	OutputTokens int     // Number of output tokens
	TTFT         float64 // Time to First Token
	TPOT         float64 // Total Processing Output Time per output token
	UseCase      string  // Field to impose fairness across labels
}

type RedisPriorityQueue struct {
	client              *redis.Client
	ctx                 context.Context
	key                 string
	mu                  sync.Mutex
	lastProcessedLabels map[string]float64 // Map to track the last virtual time processed for each label
}

func NewRedisPriorityQueue(client *redis.Client, key string) *RedisPriorityQueue {
	return &RedisPriorityQueue{
		client:              client,
		ctx:                 context.Background(),
		key:                 key,
		lastProcessedLabels: make(map[string]float64), // Initialize the map for tracking
	}
}

func (pq *RedisPriorityQueue) Push(item *Item) error {
	// Record the current time as the arrival time
	item.ArrivalTime = float64(time.Now().UnixNano()) / 1e9 // Convert to seconds

	// Calculate virtual time using the updated formula
	virtualTime := item.ArrivalTime + item.Weight/float64(item.Priority)

	// Store item with virtual time as score in the sorted set
	_, err := pq.client.ZAdd(pq.ctx, pq.key, &redis.Z{
		Score:  virtualTime,
		Member: item.ID,
	}).Result()
	if err != nil {
		return fmt.Errorf("failed to add item to sorted set: %w", err)
	}

	// Store item details in a hash for quick lookup
	itemKey := fmt.Sprintf("%s:item:%s", pq.key, item.ID)
	_, err = pq.client.HSet(pq.ctx, itemKey, map[string]interface{}{
		"ID":           item.ID,
		"Priority":     item.Priority,
		"Weight":       item.Weight,
		"ArrivalTime":  item.ArrivalTime,
		"VirtualTime":  virtualTime,
		"InputTokens":  item.InputTokens,
		"OutputTokens": item.OutputTokens,
		"TTFT":         item.TTFT,
		"TPOT":         item.TPOT,
		"UseCase":      item.UseCase,
	}).Result()
	if err != nil {
		return fmt.Errorf("failed to add item details to hash: %w", err)
	}

	return nil
}

func (pq *RedisPriorityQueue) Pop() (*Item, error) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	// Retrieve the next item based on fairness
	item, err := pq.peekOrPop(true)
	if err != nil {
		return nil, err
	}

	return item, nil
}

func (pq *RedisPriorityQueue) Peek() (*Item, error) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	// Retrieve the next item based on fairness without removing it
	item, err := pq.peekOrPop(false)
	if err != nil {
		return nil, err
	}

	return item, nil
}

// Helper method to either peek or pop the next item
func (pq *RedisPriorityQueue) peekOrPop(remove bool) (*Item, error) {
	// Retrieve the label with the oldest virtual time (round-robin approach)
	var selectedLabel string
	var minTime = math.MaxFloat64

	for label, lastTime := range pq.lastProcessedLabels {
		if lastTime < minTime {
			minTime = lastTime
			selectedLabel = label
		}
	}

	// Retrieve the next item for the selected label
	result, err := pq.client.ZRangeByScoreWithScores(pq.ctx, pq.key, &redis.ZRangeBy{
		Min: "0", Max: fmt.Sprintf("%f", math.MaxFloat64),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get items by score: %w", err)
	}
	if len(result) == 0 {
		return nil, nil
	}

	var selectedItem *redis.Z
	for _, item := range result {
		itemID := item.Member.(string)
		itemKey := fmt.Sprintf("%s:item:%s", pq.key, itemID)

		// Retrieve the item details from the hash
		itemData, err := pq.client.HGetAll(pq.ctx, itemKey).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get item details: %w", err)
		}
		if itemData["UseCase"] == selectedLabel {
			selectedItem = &item
			break
		}
	}

	if selectedItem == nil {
		// No item found for the selected label, reset label tracking and retry
		for label := range pq.lastProcessedLabels {
			pq.lastProcessedLabels[label] = 0
		}
		return pq.peekOrPop(remove)
	}

	itemID := selectedItem.Member.(string)
	itemKey := fmt.Sprintf("%s:item:%s", pq.key, itemID)

	// Retrieve the item details from the hash
	itemData, err := pq.client.HGetAll(pq.ctx, itemKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get item details: %w", err)
	}

	// Parse item details
	priority := 0
	fmt.Sscanf(itemData["Priority"], "%d", &priority)

	weight := 0.0
	fmt.Sscanf(itemData["Weight"], "%f", &weight)

	arrivalTime := 0.0
	fmt.Sscanf(itemData["ArrivalTime"], "%f", &arrivalTime)

	inputTokens := 0
	fmt.Sscanf(itemData["InputTokens"], "%d", &inputTokens)

	outputTokens := 0
	fmt.Sscanf(itemData["OutputTokens"], "%d", &outputTokens)

	ttft := 0.0
	fmt.Sscanf(itemData["TTFT"], "%f", &ttft)

	tpot := 0.0
	fmt.Sscanf(itemData["TPOT"], "%f", &tpot)

	item := &Item{
		ID:           itemID,
		Priority:     priority,
		Weight:       weight,
		ArrivalTime:  arrivalTime,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TTFT:         ttft,
		TPOT:         tpot,
		UseCase:      selectedLabel,
	}

	// Update the last processed time for the label
	if remove {
		pq.lastProcessedLabels[selectedLabel] = selectedItem.Score

		// Remove item from sorted set and hash
		_, err = pq.client.ZRem(pq.ctx, pq.key, itemID).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to remove item from sorted set: %w", err)
		}
		_, err = pq.client.Del(pq.ctx, itemKey).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to remove item details from hash: %w", err)
		}
	}

	return item, nil
}
