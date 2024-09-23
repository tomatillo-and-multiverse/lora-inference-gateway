// Package scheduling implements request scheduling algorithms.
package scheduling

import (
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
	"time"

	"go.uber.org/multierr"
	klog "k8s.io/klog/v2"

	"ext-proc/backend"
)

var (
	naiveStrategy = &Strategy{
		bests: []BestFunc{randomBestFunc},
	}
)

func NewScheduler(pmp PodMetricsProvider) *Scheduler {
	return &Scheduler{
		podMetricsProvider: pmp,
		strategies:         []*Strategy{naiveStrategy},
	}
}

type Scheduler struct {
	podMetricsProvider PodMetricsProvider
	// A list of scheduling algorithms to try. The first successful algorithm is the end result.
	strategies []*Strategy
}

// PodMetricsProvider is an interface to provide set of pods in the backend and information such as metrics.
type PodMetricsProvider interface {
	AllPodMetrics() []*backend.PodMetrics
}

// Schedule finds the target pod based on metrics and the requested lora adapter
func (s *Scheduler) Schedule(b *LLMRequest) (targetPod *backend.Pod, targetModel string, err error) {
	// TODO() Add implementation
	// Input needed: the request (model,  input size, etc.); cached pod info (current load/latency, active lora adapters, pod IP)
	// Info to be derived: latency objective, predicted output size
	var errs error
	for _, strategy := range s.strategies {
		targetPod, err = strategy.Schedule(b, s.podMetricsProvider.AllPodMetrics())
		multierr.Append(errs, err)
		if err == nil {
			return targetPod, targetModel, nil
		}
	}
	return nil, "", fmt.Errorf("failed to find target pod, this should never happen: %v", errs)
}

// A strategy picks a best target pod or returns an error.
type Strategy struct {
	// eligibilityFilters is a list of filters that filter out ineligible pods. ALL
	// eligibilityFilters MUST pass.
	eligibilityFilters []FilterFunc
	// preferenceFilters is a list of filters to filter out less favorable pods. If the preference
	// doesn't apply, the filter should return an error, allowing the algo to continue trying next
	// preference.
	preferenceFilters []FilterFunc
	// bests is a list of functions to pick the best pod. The first successful function wins.
	bests []BestFunc
}

func (s *Strategy) Schedule(b *LLMRequest, pods []*backend.PodMetrics) (targetPod *backend.Pod, err error) {
	filtered := pods
	for _, ef := range s.eligibilityFilters {
		filtered, err = ef(b, filtered)
		if err != nil {
			funcName := runtime.FuncForPC(reflect.ValueOf(ef).Pointer()).Name()
			return nil, fmt.Errorf("eligibility filter %q failed: %v", funcName, err)
		}
	}

	for _, pf := range s.preferenceFilters {
		tmp, err := pf(b, filtered)
		if err != nil {
			funcName := runtime.FuncForPC(reflect.ValueOf(pf).Pointer()).Name()
			klog.Infof("preferrence filter %q failed: %v", funcName, err)
		} else {
			filtered = tmp
		}
	}

	for _, bf := range s.bests {
		pod, err := bf(b, filtered)
		if err != nil {
			funcName := runtime.FuncForPC(reflect.ValueOf(bf).Pointer()).Name()
			klog.Infof("best func %q failed: %v", funcName, err)
		} else {
			return pod, nil
		}
	}
	return nil, fmt.Errorf("failed to schedule, this should never happen")
}

// FilterFunc filters a set of input pods to a subset.
type FilterFunc func(b *LLMRequest, pods []*backend.PodMetrics) ([]*backend.PodMetrics, error)

// BestFunc finds the best candidate pod among the input set of pods.
type BestFunc func(b *LLMRequest, pods []*backend.PodMetrics) (*backend.Pod, error)

func randomBestFunc(b *LLMRequest, pods []*backend.PodMetrics) (targetPod *backend.Pod, err error) {
	rand.Seed(time.Now().UnixNano())
	i := rand.Intn(len(pods))
	return &pods[i].Pod, nil
}
