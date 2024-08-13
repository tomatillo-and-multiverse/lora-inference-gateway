package metrics

import (
	dto "github.com/prometheus/client_model/go"

	"ext-proc/cache"
)

type Fake struct {
	Err map[cache.Pod]error
	Res map[cache.Pod]map[string]*dto.MetricFamily
}

// Fetch fetches metrics from a given pod and sends them to a channel
func (f *Fake) Fetch(pod cache.Pod) (map[string]*dto.MetricFamily, error) {
	if err, ok := f.Err[pod]; ok {
		return nil, err
	}
	return f.Res[pod], nil
}
