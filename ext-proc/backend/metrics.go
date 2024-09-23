package backend

import (
	"fmt"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"go.uber.org/multierr"
	klog "k8s.io/klog/v2"
)

const (
	ActiveLoRAAdaptersMetricName = "vllm:active_lora_adapters"
	// TODO: Replace these with the num_tokens_running/waiting below once we add those to the fork.
	RunningQueueSizeMetricName = "vllm:num_requests_running"
	WaitingQueueSizeMetricName = "vllm:num_requests_waiting"
	/* TODO: Uncomment this once the following are added to the fork.
	RunningQueueSizeMetricName        = "vllm:num_tokens_running"
	WaitingQueueSizeMetricName        = "vllm:num_tokens_waiting"
	*/
	KVCacheUsagePercentMetricName     = "vllm:gpu_cache_usage_perc"
	KvCacheMaxTokenCapacityMetricName = "vllm:gpu_cache_max_token_capacity"
)

func (p *Provider) refreshMetricsOnce() {
	start := time.Now()
	defer func() {
		d := time.Now().Sub(start)
		// TODO: add a metric instead of logging
		klog.Infof("Refreshed metrics in %v", d)
	}()
	var wg sync.WaitGroup
	processOnePod := func(key, value any) bool {
		pod := key.(Pod)
		metrics := value.(*PodMetrics)
		wg.Add(1)
		go func() {
			defer wg.Done()
			metricFamilies, err := p.pmc.FetchMetrics(pod)
			if err != nil {
				klog.Errorf("failed to parse metrics from %s: %v", pod, err)
				return
			}
			updated, err := promToPodMetrics(metricFamilies, *metrics)
			if err != nil {
				klog.Errorf("Failed to get all pod metrics updated from prometheus: %v", err)
			}
			p.UpdatePodMetrics(pod, updated)
		}()
		return true
	}
	p.podMetrics.Range(processOnePod)
	wg.Wait()
}

// promToPodMetrics converts scraped prometheus metrics to internal pod metrics.
// A combined error is returned if errors occur in one or more metric processing.
// it returns a new PodMetrics pointer which can be used to atomically update the pod metrics map.
func promToPodMetrics(metricFamilies map[string]*dto.MetricFamily, existing PodMetrics) (*PodMetrics, error) {
	var errs error
	updated := existing
	runningQueueSize, _, err := getGaugeLatestValue(metricFamilies, RunningQueueSizeMetricName)
	multierr.Append(errs, err)
	if err != nil {
		updated.RunningQueueSize = int(runningQueueSize)
	}
	waitingQueueSize, _, err := getGaugeLatestValue(metricFamilies, WaitingQueueSizeMetricName)
	multierr.Append(errs, err)
	if err != nil {
		updated.WaitingQueueSize = int(waitingQueueSize)
	}
	cachePercent, _, err := getGaugeLatestValue(metricFamilies, KVCacheUsagePercentMetricName)
	multierr.Append(errs, err)
	if err != nil {
		updated.KVCacheUsagePercent = cachePercent
	}
	kvCap, _, err := getGaugeLatestValue(metricFamilies, KvCacheMaxTokenCapacityMetricName)
	multierr.Append(errs, err)
	if err != nil {
		updated.KvCacheMaxTokenCapacity = int(kvCap)
	}
	return &updated, errs
}

// getGaugeLatestValue gets the latest value of a Gauge type metric.
func getGaugeLatestValue(metricFamilies map[string]*dto.MetricFamily, metricName string) (float64, time.Time, error) {
	mf, ok := metricFamilies[metricName]
	if !ok {
		klog.Warningf("metric family %q not found", metricName)
		return 0, time.Time{}, fmt.Errorf("metric family %q not found", metricName)
	}
	if len(mf.GetMetric()) == 0 {
		return 0, time.Time{}, fmt.Errorf("no metrics available for %q", metricName)
	}
	var latestTs int64
	var val float64
	for _, m := range mf.GetMetric() {
		if m.GetTimestampMs() > latestTs {
			latestTs = m.GetTimestampMs()
			val = m.GetGauge().GetValue()
		}
	}
	return val, time.Unix(0, latestTs*1000), nil
}

// getLabelValue returns the value of a label from a Prometheus metric
func getLabelValue(m *io_prometheus_client.Metric, label string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == label {
			return l.GetValue()
		}
	}
	return ""
}
