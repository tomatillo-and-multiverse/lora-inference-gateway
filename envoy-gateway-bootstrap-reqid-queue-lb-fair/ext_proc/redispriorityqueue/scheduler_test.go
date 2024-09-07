package redispriorityqueue

import (
	"math"
	"testing"
	"time"
)

// TestNewWFQScheduler tests the initialization of a new WFQScheduler.
func TestNewWFQScheduler(t *testing.T) {
	wfq := NewWFQScheduler()
	if wfq == nil {
		t.Fatal("Expected non-nil WFQScheduler")
	}
	if len(wfq.queues) != 0 {
		t.Fatalf("Expected no queues, got %d", len(wfq.queues))
	}
}

// TestReceive tests the addition of items to the scheduler.
func TestReceive(t *testing.T) {
	wfq := NewWFQScheduler()

	item1 := &Item{
		ID:             "item1",
		VirtualLatency: 10,
		UseCase:        "test",
	}

	wfq.Receive(item1)

	if wfq.queues["test"] == nil {
		t.Fatal("Expected queue for use case 'test' to be initialized")
	}

	if wfq.queues["test"].Len() != 1 {
		t.Fatalf("Expected queue length of 1, got %d", wfq.queues["test"].Len())
	}

	if (*wfq.queues["test"])[0].ID != "item1" {
		t.Fatalf("Expected item ID 'item1', got %s", (*wfq.queues["test"])[0].ID)
	}
}

// TestSend tests the removal of items from the scheduler.
func TestSend(t *testing.T) {
	wfq := NewWFQScheduler()

	item1 := &Item{
		ID:             "item1",
		VirtualLatency: 10,
		UseCase:        "test",
	}
	item2 := &Item{
		ID:             "item2",
		VirtualLatency: 5,
		UseCase:        "test",
	}

	wfq.Receive(item1)
	wfq.Receive(item2)

	sentItem := wfq.Send()

	if sentItem == nil {
		t.Fatal("Expected non-nil item to be sent")
	}

	if sentItem.ID != "item1" && sentItem.ID != "item2" {
		t.Fatalf("Unexpected item ID: %s", sentItem.ID)
	}

	// Verify that one item is left in the queue
	if wfq.queues["test"].Len() != 1 {
		t.Fatalf("Expected queue length of 1 after sending one item, got %d", wfq.queues["test"].Len())
	}

	//check if the item sent is the one with the lowest virtual finish time
	//see which item has the lowest virtual finish time
	if item1.VirtualFinishTime < item2.VirtualFinishTime {
		if sentItem.ID != "item1" {
			t.Fatalf("Expected item1 to be sent, got %s", sentItem.ID)
		}
	} else {
		if sentItem.ID != "item2" {
			t.Fatalf("Expected item2 to be sent, got %s", sentItem.ID)
		}
	}

}

// TestPeek tests the peek functionality of the scheduler.
func TestPeek(t *testing.T) {
	wfq := NewWFQScheduler()

	item1 := &Item{
		ID:             "item1",
		VirtualLatency: 10,
		UseCase:        "test",
	}

	wfq.Receive(item1)

	peekedID := wfq.Peek()

	if peekedID != "item1" {
		t.Fatalf("Expected peeked ID 'item1', got %s", peekedID)
	}

	// Verify that peek does not remove the item
	if wfq.queues["test"].Len() != 1 {
		t.Fatalf("Expected queue length of 1 after peeking, got %d", wfq.queues["test"].Len())
	}
}

// TestSelectUseCase tests the selection logic of use cases with the lowest virtual finish time.
func TestSelectUseCase(t *testing.T) {
	wfq := NewWFQScheduler()

	item1 := &Item{
		ID:             "item1",
		VirtualLatency: 10,
		UseCase:        "test1",
	}

	item2 := &Item{
		ID:             "item2",
		VirtualLatency: 5,
		UseCase:        "test2",
	}

	wfq.Receive(item1)
	wfq.Receive(item2)

	selectedUseCase := wfq.selectUseCase()

	if selectedUseCase != "test2" {
		t.Fatalf("Expected selected use case 'test2', got %s", selectedUseCase)
	}
}

// Helper function to compare floating-point numbers within a tolerance.
func floatEquals(a, b, tolerance float64) bool {
	return math.Abs(a-b) <= tolerance
}

// TestUpdateTime tests the update time functionality.
func TestUpdateTime(t *testing.T) {
	wfq := NewWFQScheduler()
	item := &Item{
		ID:             "item1",
		VirtualLatency: 10,
		UseCase:        "test",
	}

	// Capture current time
	now := float64(time.Now().UnixNano()) / 1e9

	wfq.updateTime(item, item.UseCase)

	if !floatEquals(item.ArrivalTime, now, 1e-3) {
		t.Fatalf("Expected arrival time to be approximately %f, got %f", now, item.ArrivalTime)
	}

	if item.VirtualFinishTime != item.VirtualTime+float64(item.VirtualLatency) {
		t.Fatalf("Expected virtual finish time %f, got %f", item.VirtualTime+float64(item.VirtualLatency), item.VirtualFinishTime)
	}
}
