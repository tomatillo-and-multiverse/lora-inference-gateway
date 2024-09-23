package backend

import (
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	klog "k8s.io/klog/v2"
)

const (
	refreshPodsInterval    = 10 * time.Second
	refreshMetricsInterval = 50 * time.Millisecond
)

func NewProvider(pmc PodMetricsClient, pl PodLister) *Provider {
	p := &Provider{
		podMetrics: sync.Map{},
		pmc:        pmc,
		pl:         pl,
	}
	go p.init()
	return p
}

// Provider provides backend pods and information such as metrics.
type Provider struct {
	// key: Pod, value: *PodMetrics
	podMetrics sync.Map
	pmc        PodMetricsClient
	pl         PodLister
}

type PodMetricsClient interface {
	FetchMetrics(pod Pod) (map[string]*dto.MetricFamily, error)
}

type PodLister interface {
	List() (PodSet, error)
}

func (p *Provider) AllPodMetrics() []*PodMetrics {
	res := []*PodMetrics{}
	fn := func(k, v any) bool {
		res = append(res, v.(*PodMetrics))
		return true
	}
	p.podMetrics.Range(fn)
	return res
}

func (p *Provider) UpdatePodMetrics(pod Pod, pm *PodMetrics) {
	p.podMetrics.Store(pod, pm)
}

func (p *Provider) GetPodMetrics(pod Pod) (*PodMetrics, bool) {
	val, ok := p.podMetrics.Load(pod)
	if ok {
		return val.(*PodMetrics), true
	}
	return nil, false
}

func (p *Provider) init() {
	p.refreshPodsOnce()
	p.refreshMetricsOnce()

	// periodically refresh pods
	go func() {
		for {
			time.Sleep(refreshPodsInterval)
			if err := p.refreshPodsOnce(); err != nil {
				klog.Errorf("Failed to list pods: %v", err)
			}
		}
	}()

	// periodically refresh metrics
	go func() {
		for {
			time.Sleep(refreshMetricsInterval)
			p.refreshMetricsOnce()
		}
	}()
}

// refreshPodsOnce lists pods and updates keys in the podMetrics map.
// Note this function doesn't update the PodMetrics value, it's done separately.
func (p *Provider) refreshPodsOnce() error {
	pods, err := p.pl.List()
	if err != nil {
		return err
	}
	// merge new pods with cached ones.
	// add new pod to the map
	for pod := range pods {
		if _, ok := p.podMetrics.Load(pod); !ok {
			p.podMetrics.Store(pod, &PodMetrics{Pod: pod})
		}
	}
	// remove pods that don't exist any more.
	mergeFn := func(k, v any) bool {
		pod := k.(Pod)
		if _, ok := pods[pod]; !ok {
			p.podMetrics.Delete(pod)
		}
		return true
	}
	p.podMetrics.Range(mergeFn)
	return nil
}
