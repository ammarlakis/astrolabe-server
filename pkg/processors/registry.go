package processors

import (
	"github.com/ammarlakis/astrolabe/pkg/graph"
	"k8s.io/klog/v2"
)

// EventType represents the type of Kubernetes event
type EventType string

const (
	EventAdd    EventType = "ADD"
	EventUpdate EventType = "UPDATE"
	EventDelete EventType = "DELETE"
)

// Processor processes Kubernetes resources and updates the graph
type Processor interface {
	Process(obj interface{}, eventType EventType) error
}

// ProcessorRegistry manages all resource processors
type ProcessorRegistry struct {
	graph      graph.GraphInterface
	processors map[string]Processor
}

// NewProcessorRegistry creates a new processor registry
func NewProcessorRegistry(g graph.GraphInterface) *ProcessorRegistry {
	registry := &ProcessorRegistry{
		graph:      g,
		processors: make(map[string]Processor),
	}

	// Register all processors
	registry.registerProcessors()

	return registry
}

// registerProcessors registers all resource type processors
func (r *ProcessorRegistry) registerProcessors() {
	// Core resources
	type pair struct {
		kind      string
		processor Processor
	}

	processors := []pair{
		{"Pod", NewPodProcessor(r.graph)},
		{"Service", NewServiceProcessor(r.graph)},
		{"ServiceAccount", NewServiceAccountProcessor(r.graph)},
		{"ConfigMap", NewConfigMapProcessor(r.graph)},
		{"Secret", NewSecretProcessor(r.graph)},
		{"PersistentVolumeClaim", NewPVCProcessor(r.graph)},
		{"PersistentVolume", NewPVProcessor(r.graph)},
		{"Namespace", NewNamespaceProcessor(r.graph)},

		{"Deployment", NewDeploymentProcessor(r.graph)},
		{"StatefulSet", NewStatefulSetProcessor(r.graph)},
		{"DaemonSet", NewDaemonSetProcessor(r.graph)},
		{"ReplicaSet", NewReplicaSetProcessor(r.graph)},

		{"Job", NewJobProcessor(r.graph)},
		{"CronJob", NewCronJobProcessor(r.graph)},

		{"Ingress", NewIngressProcessor(r.graph)},
		{"EndpointSlice", NewEndpointSliceProcessor(r.graph)},

		{"StorageClass", NewStorageClassProcessor(r.graph)},

		{"HorizontalPodAutoscaler", NewHPAProcessor(r.graph)},

		{"PodDisruptionBudget", NewPDBProcessor(r.graph)},
	}

	for _, processor := range processors {
		r.processors[processor.kind] = processor.processor
	}
}

// Process processes a resource event
func (r *ProcessorRegistry) Process(obj interface{}, kind string, eventType EventType) {
	processor, exists := r.processors[kind]
	if !exists {
		klog.V(4).Infof("No processor registered for kind: %s", kind)
		return
	}

	if err := processor.Process(obj, eventType); err != nil {
		klog.Errorf("Failed to process %s event for %s: %v", eventType, kind, err)
	}
}
