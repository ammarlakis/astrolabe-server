package processors

import (
	"github.com/ammarlakis/astrolabe/pkg/graph"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

// EventType represents the type of Kubernetes event
type EventType string

const (
	EventAdd    EventType = "add"
	EventUpdate EventType = "update"
	EventDelete EventType = "delete"
)

// Processor processes Kubernetes resources and updates the graph
type Processor interface {
	Process(obj interface{}, eventType EventType) error
}

// GraphInterface defines the interface for graph operations
type GraphInterface interface {
	AddNode(node *graph.Node)
	RemoveNode(uid types.UID)
	GetNode(uid types.UID) (*graph.Node, bool)
	AddEdge(edge *graph.Edge) bool
	RemoveEdge(fromUID, toUID types.UID)
	GetAllNodes() []*graph.Node
	GetNodesByNamespaceKind(namespace, kind string) []*graph.Node
}

// ProcessorRegistry manages all resource processors
type ProcessorRegistry struct {
	graph      GraphInterface
	processors map[string]Processor
}

// NewProcessorRegistry creates a new processor registry
func NewProcessorRegistry(g GraphInterface) *ProcessorRegistry {
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
	r.processors["Pod"] = NewPodProcessor(r.graph)
	r.processors["Service"] = NewServiceProcessor(r.graph)
	r.processors["ServiceAccount"] = NewServiceAccountProcessor(r.graph)
	r.processors["ConfigMap"] = NewConfigMapProcessor(r.graph)
	r.processors["Secret"] = NewSecretProcessor(r.graph)
	r.processors["PersistentVolumeClaim"] = NewPVCProcessor(r.graph)
	r.processors["PersistentVolume"] = NewPVProcessor(r.graph)
	r.processors["Namespace"] = NewNamespaceProcessor(r.graph)
	
	// Apps resources
	r.processors["Deployment"] = NewDeploymentProcessor(r.graph)
	r.processors["StatefulSet"] = NewStatefulSetProcessor(r.graph)
	r.processors["DaemonSet"] = NewDaemonSetProcessor(r.graph)
	r.processors["ReplicaSet"] = NewReplicaSetProcessor(r.graph)
	
	// Batch resources
	r.processors["Job"] = NewJobProcessor(r.graph)
	r.processors["CronJob"] = NewCronJobProcessor(r.graph)
	
	// Networking resources
	r.processors["Ingress"] = NewIngressProcessor(r.graph)
	r.processors["EndpointSlice"] = NewEndpointSliceProcessor(r.graph)
	
	// Storage resources
	r.processors["StorageClass"] = NewStorageClassProcessor(r.graph)
	
	// Autoscaling resources
	r.processors["HorizontalPodAutoscaler"] = NewHPAProcessor(r.graph)
	
	// Policy resources
	r.processors["PodDisruptionBudget"] = NewPDBProcessor(r.graph)
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
