package redispriorityqueue

import (
    "context"
    "fmt"
    "log"
    "sync"

    "github.com/go-redis/redis/v8"
)

type Item struct {
    ID       string
    Priority int
}

type RedisPriorityQueue struct {
    client *redis.Client
    ctx    context.Context
    key    string
    mu     sync.Mutex
}

func NewRedisPriorityQueue(client *redis.Client, key string) *RedisPriorityQueue {
    return &RedisPriorityQueue{
        client: client,
        ctx:    context.Background(),
        key:    key,
    }
}

func (pq *RedisPriorityQueue) Push(item *Item) error {
    priorityKey := fmt.Sprintf("%s:priority:%d", pq.key, item.Priority)

    // Atomic operation: ZAdd and HSet are atomic in Redis
    _, err := pq.client.ZAdd(pq.ctx, pq.key, &redis.Z{
        Score:  float64(item.Priority),
        Member: priorityKey,
    }).Result()
    if err != nil {
        return fmt.Errorf("failed to add priority to sorted set: %w", err)
    }

    _, err = pq.client.HSet(pq.ctx, priorityKey, item.ID, item.ID).Result()
    if err != nil {
        return fmt.Errorf("failed to add item to hash map: %w", err)
    }

    return nil
}

func (pq *RedisPriorityQueue) Pop() ([]*Item, error) {
    pq.mu.Lock()
    defer pq.mu.Unlock()

    result, err := pq.client.ZRevRangeWithScores(pq.ctx, pq.key, 0, 0).Result()
    if err != nil {
        return nil, fmt.Errorf("failed to get top priority: %w", err)
    }
    if len(result) == 0 {
        return nil, nil
    }

    highestPriorityKey := result[0].Member.(string)

    // Fetch and remove items atomically
    itemsData, err := pq.client.HGetAll(pq.ctx, highestPriorityKey).Result()
    if err != nil {
        return nil, fmt.Errorf("failed to get items with top priority: %w", err)
    }

    if len(itemsData) == 0 {
        return nil, nil
    }


    items := make([]*Item, 0, len(itemsData))
    ids := make([]string, 0, len(itemsData))
    for id := range itemsData {
        items = append(items, &Item{
            ID:       id,
            Priority: int(result[0].Score),
        })
        ids = append(ids, id)
    }
    // Delete items and the priority key atomically
    pipeline := pq.client.TxPipeline()
    pipeline.HDel(pq.ctx, highestPriorityKey, ids...)
    pipeline.ZRem(pq.ctx, pq.key, highestPriorityKey)
    _, err = pipeline.Exec(pq.ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to remove items and priority key: %w", err)
    }

    return items, nil
}

func (pq *RedisPriorityQueue) Top() ([]*Item, error) {
    pq.mu.Lock()
    defer pq.mu.Unlock()

    result, err := pq.client.ZRevRangeWithScores(pq.ctx, pq.key, 0, 0).Result()
    if err != nil {
        return nil, fmt.Errorf("failed to get top priority: %w", err)
    }
    if len(result) == 0 {
        return nil, nil
    }

    highestPriorityKey := result[0].Member.(string)

    itemsData, err := pq.client.HGetAll(pq.ctx, highestPriorityKey).Result()
    if err != nil {
        return nil, fmt.Errorf("failed to get items with top priority: %w", err)
    }

    items := make([]*Item, 0, len(itemsData))
    for id := range itemsData {
        items = append(items, &Item{
            ID:       id,
            Priority: int(result[0].Score),
        })
    }

    return items, nil
}

func (pq *RedisPriorityQueue) IsTop(id string) (bool, error) {
    pq.mu.Lock()
    defer pq.mu.Unlock()

    result, err := pq.client.ZRevRangeWithScores(pq.ctx, pq.key, 0, 0).Result()
    if err != nil {
        return false, fmt.Errorf("failed to get top priority: %w", err)
    }
    if len(result) == 0 {
        return false, nil
    }

    highestPriorityKey := result[0].Member.(string)

    exists, err := pq.client.HExists(pq.ctx, highestPriorityKey, id).Result()
    if err != nil {
        return false, fmt.Errorf("failed to check if item exists in hash map: %w", err)
    }

    return exists, nil
}

func (pq *RedisPriorityQueue) DeleteByID(id string) error {
    pq.mu.Lock()
    defer pq.mu.Unlock()

    // Get all priority levels
    priorityLevels, err := pq.client.ZRange(pq.ctx, pq.key, 0, -1).Result()
    if err != nil {
        return fmt.Errorf("failed to get priority levels: %w", err)
    }

    for _, priorityKey := range priorityLevels {
        // Check if the item exists in this priority level
        exists, err := pq.client.HExists(pq.ctx, priorityKey, id).Result()
        if err != nil {
            return fmt.Errorf("failed to check if item exists in hash map: %w", err)
        }

        if exists {
            // Delete the item from the hash
            _, err = pq.client.HDel(pq.ctx, priorityKey, id).Result()
            if err != nil {
                return fmt.Errorf("failed to remove item from hash map: %w", err)
            }

            // Check if the hash is now empty
            size, err := pq.client.HLen(pq.ctx, priorityKey).Result()
            if err != nil {
                return fmt.Errorf("failed to get hash size: %w", err)
            }

            // If the hash is empty, remove the priority from the sorted set
            if size == 0 {
                _, err = pq.client.ZRem(pq.ctx, pq.key, priorityKey).Result()
                if err != nil {
                    return fmt.Errorf("failed to remove priority from sorted set: %w", err)
                }
            }

            log.Printf("Deleted item ID: %s from priority level: %s", id, priorityKey)
            return nil
        }
    }

    // If we got here, the item wasn't found
    log.Printf("Item ID: %s not found in any priority level", id)
    return nil
}
