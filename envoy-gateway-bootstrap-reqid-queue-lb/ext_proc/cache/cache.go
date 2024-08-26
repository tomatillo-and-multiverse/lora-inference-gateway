package cache

import (
	"encoding/json"
	"fmt"
	"time"
	"github.com/coocood/freecache"
)

type ActiveLoraModelMetrics struct {
	Date                    string
	PodName                 string
	ModelName               string
	NumberOfPendingRequests int

}

type PendingRequestActiveAdaptersMetrics struct {
	Date                   string
	PodName                string
	PendingRequests        int
	RunningRequests        int
	WaitingRequests        int
	NumberOfActiveAdapters int
	BaseModel              string
	GPUKVCacheUsagePerc float64
}




type LLMRequest struct {
	IP						  string
	PodName					  string
	SentTime                    string
	ReceivedTime                string
	ReqID		   			string
	TokensSent			    int
	TokensReceived          int
	TokensPending			    int
	ModelName				string
	BaseModel				string
	Latency						time.Duration
}



func SetCacheLLMRequests(cache *freecache.Cache, metric LLMRequest, expiration int) error {
	cacheKey := fmt.Sprintf("%s:", metric.ReqID)
	cacheValue, err := json.Marshal(metric)
	if err != nil {
		return fmt.Errorf("error marshaling LLMRequest for key %s: %v", cacheKey, err)
	}
	err = cache.Set([]byte(cacheKey), cacheValue, expiration)
	if err != nil {
		return fmt.Errorf("error setting LLMRequest for key %s: %v", cacheKey, err)
	}
	//fmt.Printf("Set LLMRequest - Key: %s, Value: %s\n", cacheKey, cacheValue)
	return nil
}

func GetCacheLLMRequests(cache *freecache.Cache, ID string) (*LLMRequest, error) {
	cacheKey := fmt.Sprintf("%s:", ID)

	value, err := cache.Get([]byte(cacheKey))
	if err != nil {
		return nil, fmt.Errorf("error fetching LLMRequest for key %s: %v", cacheKey, err)
	}
	var metric LLMRequest
	err = json.Unmarshal(value, &metric)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling LLMRequest for key %s: %v", cacheKey, err)
	}
	//fmt.Printf("Got LLMRequest - Key: %s, Value: %s\n", cacheKey, value)
	return &metric, nil
}


func SetCacheActiveLoraModel(cache *freecache.Cache, metric ActiveLoraModelMetrics) error {
	cacheKey := fmt.Sprintf("%s:%s", metric.PodName, metric.ModelName)
	cacheValue, err := json.Marshal(metric)
	if err != nil {
		return fmt.Errorf("error marshaling ActiveLoraModelMetrics for key %s: %v", cacheKey, err)
	}
	err = cache.Set([]byte(cacheKey), cacheValue, 0)
	if err != nil {
		return fmt.Errorf("error setting cacheActiveLoraModel for key %s: %v", cacheKey, err)
	}
	fmt.Printf("Set cacheActiveLoraModel - Key: %s, Value: %s\n", cacheKey, cacheValue)
	return nil
}

func SetCachePendingRequestActiveAdapters(cache *freecache.Cache, metric PendingRequestActiveAdaptersMetrics) error {
	cacheKey := fmt.Sprintf("%s:", metric.PodName)
	cacheValue, err := json.Marshal(metric)
	if err != nil {
		return fmt.Errorf("error marshaling PendingRequestActiveAdaptersMetrics for key %s: %v", cacheKey, err)
	}
	err = cache.Set([]byte(cacheKey), cacheValue, 0)
	if err != nil {
		return fmt.Errorf("error setting cachePendingRequestActiveAdapters for key %s: %v", cacheKey, err)
	}
	//fmt.Printf("Set cachePendingRequestActiveAdapters - Key: %s, Value: %s\n", cacheKey, cacheValue)
	return nil
}

func GetCacheActiveLoraModel(cache *freecache.Cache, podName, modelName string) (*ActiveLoraModelMetrics, error) {
	cacheKey := fmt.Sprintf("%s:%s", podName, modelName)

	value, err := cache.Get([]byte(cacheKey))
	if err != nil {
		return nil, fmt.Errorf("error fetching cacheActiveLoraModel for key %s: %v", cacheKey, err)
	}
	var metric ActiveLoraModelMetrics
	err = json.Unmarshal(value, &metric)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling ActiveLoraModelMetrics for key %s: %v", cacheKey, err)
	}
	//fmt.Printf("Got cacheActiveLoraModel - Key: %s, Value: %s\n", cacheKey, value)
	return &metric, nil
}

func GetCachePendingRequestActiveAdapters(cache *freecache.Cache, podName string) (*PendingRequestActiveAdaptersMetrics, error) {
	cacheKey := fmt.Sprintf("%s:", podName)

	value, err := cache.Get([]byte(cacheKey))
	if err != nil {
		return nil, fmt.Errorf("error fetching cachePendingRequestActiveAdapters for key %s: %v", cacheKey, err)
	}
	var metric PendingRequestActiveAdaptersMetrics
	err = json.Unmarshal(value, &metric)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling PendingRequestActiveAdaptersMetrics for key %s: %v", cacheKey, err)
	}
	//fmt.Printf("Got cachePendingRequestActiveAdapters - Key: %s, Value: %s\n", cacheKey, value)
	return &metric, nil
}

func GetBaseModel(cache *freecache.Cache, podName string) (string, error) {
	cacheKey := fmt.Sprintf("%s:", podName)

	value, err := cache.Get([]byte(cacheKey))
	if err != nil {
		return "", fmt.Errorf("error fetching baseModel for key %s: %v", cacheKey, err)
	}
	var metric PendingRequestActiveAdaptersMetrics
	err = json.Unmarshal(value, &metric)
	if err != nil {
		return "", fmt.Errorf("error unmarshaling baseModel for key %s: %v", cacheKey, err)
	}
	//fmt.Printf("Got baseModel - Key: %s, Value: %s\n", cacheKey, value)
	return metric.BaseModel, nil
}
