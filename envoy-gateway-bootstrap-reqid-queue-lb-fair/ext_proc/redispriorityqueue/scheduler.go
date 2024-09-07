package redispriorityqueue

import (
	"container/heap"
	"math"
	"sync"
	"time"
)

// Item represents a network item (packet) in the WFQScheduler.
type Item struct {
	ID                string  // Unique identifier for the item
	VirtualLatency    float64 // Latency of the item
	ArrivalTime       float64 // Arrival time of the item
	VirtualTime       float64 // Virtual start time
	VirtualFinishTime float64 // Virtual finish time
	UseCase           string  // Specific use case identifier or description
}

// ItemQueue is a priority queue of items.
type ItemQueue []*Item

func (pq ItemQueue) Len() int { return len(pq) }

func (pq ItemQueue) Less(i, j int) bool {
	return pq[i].VirtualFinishTime < pq[j].VirtualFinishTime
}

func (pq ItemQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *ItemQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*Item))
}

func (pq *ItemQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

// WFQScheduler represents the Weighted Fair Queuing scheduler.
type WFQScheduler struct {
	queues        map[string]*ItemQueue // Map of use cases to their corresponding queues
	lastVirFinish map[string]float64    // Map of use cases to their last virtual finish times
	mu            sync.RWMutex          // RWMutex to allow concurrent reads and exclusive writes
}

// NewWFQScheduler initializes a new WFQScheduler.
func NewWFQScheduler() *WFQScheduler {
	return &WFQScheduler{
		queues:        make(map[string]*ItemQueue),
		lastVirFinish: make(map[string]float64),
	}
}

// Receive handles an incoming item and enqueues it to the appropriate queue based on its use case.
func (wfq *WFQScheduler) Receive(item *Item) {
	wfq.mu.Lock() // Acquire write lock
	defer wfq.mu.Unlock()

	queue, exists := wfq.queues[item.UseCase]
	if !exists {
		// Create a new queue for the use case if it does not exist
		newQueue := &ItemQueue{}
		heap.Init(newQueue)
		wfq.queues[item.UseCase] = newQueue
		wfq.lastVirFinish[item.UseCase] = 0
		queue = newQueue
	}

	wfq.updateTime(item, item.UseCase)
	heap.Push(queue, item)
}

// updateTime updates the virtual time for the item.
func (wfq *WFQScheduler) updateTime(item *Item, useCase string) {
	now := float64(time.Now().UnixNano()) / 1e9
	virStart := math.Max(now, wfq.lastVirFinish[useCase])
	item.ArrivalTime = now
	item.VirtualTime = virStart
	item.VirtualFinishTime = virStart + float64(item.VirtualLatency)
	wfq.lastVirFinish[useCase] = item.VirtualFinishTime
}

// Send selects and dequeues the next item to be sent.
func (wfq *WFQScheduler) Send() *Item {
	wfq.mu.RLock() // Acquire write lock
	defer wfq.mu.RUnlock()

	//log.Printf("will select use case")
	useCase := wfq.selectUseCase()
	if useCase == "" {
		return nil // No item to send
	}

	//log.Printf("selected use case %s\n", useCase)

	queue := wfq.queues[useCase]
	item := heap.Pop(queue).(*Item)
	//log.Printf("Sending item %s from use case %s\n", item.ID, useCase)
	if queue.Len() == 0 {
		delete(wfq.queues, useCase) // Clean up empty queues
		delete(wfq.lastVirFinish, useCase)
	}
	return item
}

// Peek returns the ID of the top item in the queue with the lowest virtual finish time without removing it.
func (wfq *WFQScheduler) Peek() string {
	wfq.mu.RLock() // Acquire read lock
	defer wfq.mu.RUnlock()

	useCase := wfq.selectUseCase()
	if useCase == "" {
		return "" // No item to peek at
	}

	queue := wfq.queues[useCase]
	if queue.Len() > 0 {
		return (*queue)[0].ID
	}
	return ""
}

func (wfq *WFQScheduler) selectUseCase() string {
	wfq.mu.RLock() // Acquire write lock to prevent race condition
	defer wfq.mu.RUnlock()
	//log.Printf("selecting use case")
	minVirFinish := math.Inf(1)
	selectedUseCase := ""

	for useCase, queue := range wfq.queues {
		//log.Printf("use case %s has %d items\n", useCase, queue.Len())
		if queue.Len() > 0 && (*queue)[0].VirtualFinishTime < minVirFinish {
			minVirFinish = (*queue)[0].VirtualFinishTime
			selectedUseCase = useCase
		}
	}

	return selectedUseCase
}
