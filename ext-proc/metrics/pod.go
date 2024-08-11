package metrics

import (
	"fmt"
	"net/http"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	klog "k8s.io/klog/v2"

	"ext-proc/cache"
)

type PodMetrics struct {
}

// Fetch fetches metrics from a given pod and sends them to a channel
func (p *PodMetrics) Fetch(pod cache.Pod) (map[string]*dto.MetricFamily, error) {
	url := fmt.Sprintf("http://%s/metrics", pod.Address)
	resp, err := http.Get(url)
	if err != nil {
		klog.Errorf("failed to fetch metrics from %s: %v", pod, err)
		return nil, fmt.Errorf("failed to fetch metrics from %s: %w", pod, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		klog.Errorf("unexpected status code from %s: %v", pod, resp.StatusCode)
		return nil, fmt.Errorf("unexpected status code from %s: %v", pod, resp.StatusCode)
	}

	parser := expfmt.TextParser{}
	return parser.TextToMetricFamilies(resp.Body)
}
