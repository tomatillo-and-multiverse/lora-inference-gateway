package cache

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"

	"github.com/coocood/freecache" // This import is missing
	"github.com/hashicorp/golang-lru/v2/expirable"
)

type ActiveLoraModelMetrics struct {
	Date                    string
	PodName                 string
	ModelName               string
	NumberOfPendingRequests int
}

type PendingRequestActiveAdaptersMetrics struct {
	Date                      string
	PodName                   string
	PendingRequests           int
	RunningRequests           int
	WaitingRequests           int
	NumberOfActiveAdapters    int
	BaseModel                 string
	GPUKVCacheUsagePerc       float64
	PseudoGPUKVCacheUsagePerc float64
}

type LLMRequest struct {
	IP                  string
	PodName             string
	SentTime            string
	ReceivedTime        string
	ReqID               string
	UseCase             string
	TokensSent          int
	TokensReceived      int
	TokensPending       int
	ModelName           string
	BaseModel           string
	E2ELatencyInSec     float64
	PrefillLatencyInSec float64
}

type PodUseCaseMetrics struct {
	PodName                       string
	ModelName                     string
	BaseModel                     string
	UseCase                       string
	PrefillLatenciesInSecPerToken []float64
	DecodeLatenciesInSecPerToken  []float64
	E2ELatenciesInSecPerToken     []float64
	TokensSent                    int
	TokensReceived                int
	TokensPending                 int
	GPUKVCacheUsagePerc           float64 // pod level
	PseudoGPUKVCacheUsagePerc     float64 // pod level
}

func GetReqIDsFromCacheKey() string {
	return "reqIDs"
}

func serializeList(data interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(data)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func deserializeList(data []byte, v interface{}) error {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	return dec.Decode(v)
}

func SetReqIDCache(cache *freecache.Cache, reqIDs []string) error {
	var serializedList []byte
	var err error
	if serializedList, err = serializeList(reqIDs); err != nil {
		return fmt.Errorf("error serializing value for key %s: %v", GetReqIDsFromCacheKey(), err)
	}
	if err = cache.Set([]byte(GetReqIDsFromCacheKey()), serializedList, 0); err != nil {
		return fmt.Errorf("error setting value for key %s: %v", GetReqIDsFromCacheKey(), err)
	}
	return nil
}

func AppendReqIDToCache(cache *freecache.Cache, reqID string) error {
	value, err := cache.Get([]byte(GetReqIDsFromCacheKey()))
	if err != nil {
		return fmt.Errorf("error fetching value for key %s: %v", GetReqIDsFromCacheKey(), err)
	}
	var reqIDs []string
	if value != nil {
		err = deserializeList(value, &reqIDs)
		if err != nil {
			return fmt.Errorf("error deserializing value for key %s: %v", GetReqIDsFromCacheKey(), err)
		}
	}
	reqIDs = append(reqIDs, reqID)
	value, err = serializeList(reqIDs)
	if err != nil {
		return fmt.Errorf("error serializing value for key %s: %v", GetReqIDsFromCacheKey(), err)
	}
	err = cache.Set([]byte(GetReqIDsFromCacheKey()), value, 0)
	if err != nil {
		return fmt.Errorf("error setting value for key %s: %v", GetReqIDsFromCacheKey(), err)
	}
	return nil
}

func GetReqIDsFromCache(cache *freecache.Cache) ([]string, error) {
	value, err := cache.Get([]byte(GetReqIDsFromCacheKey()))
	if err != nil {
		return nil, fmt.Errorf("error fetching value for key %s: %v", GetReqIDsFromCacheKey(), err)
	}
	var reqIDs []string
	if value != nil {
		err = deserializeList(value, &reqIDs)
		if err != nil {
			return nil, fmt.Errorf("error deserializing value for key %s: %v", GetReqIDsFromCacheKey(), err)
		}
	}
	return reqIDs, nil
}

func SetPodUseCaseMetrics(cache *freecache.Cache, metric PodUseCaseMetrics, cacheKey string) error {
	cacheValue, err := json.Marshal(metric)
	if err != nil {
		return fmt.Errorf("error marshaling PodUseCaseMetrics for key %s: %v", cacheKey, err)
	}
	err = cache.Set([]byte(cacheKey), cacheValue, 0)
	if err != nil {
		return fmt.Errorf("error setting PodUseCaseMetrics for key %s: %v", cacheKey, err)
	}
	//fmt.Printf("Set LLMRequest - Key: %s, Value: %s\n", cacheKey, cacheValue)
	return nil
}

func GetPodUseCaseMetrics(cache *freecache.Cache, cacheKey string) (*PodUseCaseMetrics, error) {

	value, err := cache.Get([]byte(cacheKey))
	if err != nil {
		return nil, fmt.Errorf("error fetching PodUseCaseMetrics for key %s: %v", cacheKey, err)
	}
	var metric PodUseCaseMetrics
	err = json.Unmarshal(value, &metric)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling PodUseCaseMetrics for key %s: %v", cacheKey, err)
	}
	//fmt.Printf("Got LLMRequest - Key: %s, Value: %s\n", cacheKey, value)
	return &metric, nil
}

func SetLRUCacheLLMRequest(cache *expirable.LRU[string, LLMRequest], req LLMRequest) {
	cache.Add(req.ReqID, req)
	//fmt.Printf("Stored LLMRequest with ReqID: %s\n", req.ReqID)
}

// GetLLMRequest retrieves an LLMRequest from the LRU cache using ReqID
func GetLRUCacheLLMRequest(cache *expirable.LRU[string, LLMRequest], reqID string) (*LLMRequest, bool) {
	if req, ok := cache.Get(reqID); ok {
		//fmt.Printf("Retrieved LLMRequest with ReqID: %s\n", reqID)
		return &req, true
	}
	//fmt.Printf("LLMRequest with ReqID: %s not found in cache\n", reqID)
	return &LLMRequest{}, false
}

func DeleteLRUCacheLLMRequest(cache *expirable.LRU[string, LLMRequest], reqID string) bool {
	ok := cache.Remove(reqID)
	if !ok {
		fmt.Printf("[DeleteLRUCacheLLMRequest] LLMRequest with ReqID: %s not found in cache\n", reqID)
		return false
	}
	//fmt.Printf("Deleted LLMRequest with ReqID: %s\n", reqID)
	return true
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
	//fmt.Printf("Set cacheActiveLoraModel - Key: %s, Value: %s\n", cacheKey, cacheValue)
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
