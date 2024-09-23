// Package backend is a library to interact with backend model servers such as probing metrics.
package backend

type PodSet map[Pod]bool

type Pod struct {
	Namespace string
	Name      string
	Address   string
}

func (p Pod) String() string {
	return p.Namespace + "." + p.Name
}

type Metrics struct {
	// ActiveLoRAAdapters is a set of LoRA adapters that are currently loaded to GPU memory.
	ActiveLoRAAdapters      map[string]bool
	RunningQueueSize        int
	WaitingQueueSize        int
	KVCacheUsagePercent     float64
	KvCacheMaxTokenCapacity int
}

type PodMetrics struct {
	Pod
	Metrics
}
