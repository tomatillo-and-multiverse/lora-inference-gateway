package cache

import (
	"encoding/json"
	"fmt"

	"github.com/coocood/freecache"
	klog "k8s.io/klog/v2"
)

type Pod struct {
	Namespace string
	Name      string
	Address   string
}

func (p Pod) String() string {
	return p.Namespace + "." + p.Name
}

type ActiveLoraModelMetrics struct {
	Date                    string
	Pod                     Pod
	ModelName               string
	NumberOfPendingRequests int
}

type PendingRequestActiveAdaptersMetrics struct {
	Date                   string
	Pod                    Pod
	PendingRequests        int
	NumberOfActiveAdapters int
}

func SetCacheActiveLoraModel(cache *freecache.Cache, metric ActiveLoraModelMetrics) error {
	cacheKey := fmt.Sprintf("%s:%s", metric.Pod, metric.ModelName)
	cacheValue, err := json.Marshal(metric)
	if err != nil {
		return fmt.Errorf("error marshaling ActiveLoraModelMetrics for key %s: %v", cacheKey, err)
	}
	err = cache.Set([]byte(cacheKey), cacheValue, 0)
	if err != nil {
		return fmt.Errorf("error setting cacheActiveLoraModel for key %s: %v", cacheKey, err)
	}
	klog.V(2).Infof("Set cacheActiveLoraModel - Key: %s, Value: %s\n", cacheKey, cacheValue)
	return nil
}

func SetCachePendingRequestActiveAdapters(cache *freecache.Cache, metric PendingRequestActiveAdaptersMetrics) error {
	cacheKey := fmt.Sprintf("%s:", metric.Pod)
	cacheValue, err := json.Marshal(metric)
	if err != nil {
		return fmt.Errorf("error marshaling PendingRequestActiveAdaptersMetrics for key %s: %v", cacheKey, err)
	}
	err = cache.Set([]byte(cacheKey), cacheValue, 0)
	if err != nil {
		return fmt.Errorf("error setting cachePendingRequestActiveAdapters for key %s: %v", cacheKey, err)
	}
	klog.V(2).Infof("Set cachePendingRequestActiveAdapters - Key: %s, Value: %s\n", cacheKey, cacheValue)
	return nil
}

func GetCacheActiveLoraModel(cache *freecache.Cache, pod Pod, modelName string) (*ActiveLoraModelMetrics, error) {
	cacheKey := fmt.Sprintf("%s:%s", pod, modelName)

	value, err := cache.Get([]byte(cacheKey))
	if err != nil {
		return nil, fmt.Errorf("error fetching cacheActiveLoraModel for key %s: %v", cacheKey, err)
	}
	var metric ActiveLoraModelMetrics
	err = json.Unmarshal(value, &metric)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling ActiveLoraModelMetrics for key %s: %v", cacheKey, err)
	}
	klog.V(2).Infof("Got cacheActiveLoraModel - Key: %s, Value: %s\n", cacheKey, value)
	return &metric, nil
}

func GetCachePendingRequestActiveAdapters(cache *freecache.Cache, pod Pod) (*PendingRequestActiveAdaptersMetrics, error) {
	cacheKey := fmt.Sprintf("%s:", pod)

	value, err := cache.Get([]byte(cacheKey))
	if err != nil {
		return nil, fmt.Errorf("error fetching cachePendingRequestActiveAdapters for key %s: %v", cacheKey, err)
	}
	var metric PendingRequestActiveAdaptersMetrics
	err = json.Unmarshal(value, &metric)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling PendingRequestActiveAdaptersMetrics for key %s: %v", cacheKey, err)
	}
	klog.V(2).Infof("Got cachePendingRequestActiveAdapters - Key: %s, Value: %s\n", cacheKey, value)
	return &metric, nil
}
