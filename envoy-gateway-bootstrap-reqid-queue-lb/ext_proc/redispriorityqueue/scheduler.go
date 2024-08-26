package redispriorityqueue

import (
	"log"
	"time"

)

// AddItemToQueue adds the request item to the priority queue
func AddItemToQueue(pq *RedisPriorityQueue, item *Item) error {
	err := pq.Push(item)
	if err != nil {
		log.Printf("Error pushing item to the queue: %v", err)
		return err
	}
	return nil
}

// WaitForTopItem waits until the given item is at the top of the queue or until the timeout is reached
func WaitForTopItem(pq *RedisPriorityQueue, item *Item) bool {
	timeout := time.After(3 * time.Second)
	breakLoop: // Define a label
	for {
		select {
		case <-timeout:
			log.Printf("Timeout reached while waiting for top item with ID: %s", item.ID)
			pq.DeleteByID(item.ID)
			break breakLoop // Exit the loop on timeout
		default:
			isTopItem, err := pq.IsTop(item.ID)
			if err != nil {
				log.Printf("Error fetching top item from the queue: %v", err)
				break breakLoop // Exit the loop on error
			}
			log.Printf("Is %s a top item: %v", item.ID, isTopItem)
			if isTopItem {
				pq.DeleteByID(item.ID)
				break breakLoop // Exit the loop when the item is at the top of the queue
			}
			time.Sleep(3 * time.Millisecond)
		}
	}
	return true
}
