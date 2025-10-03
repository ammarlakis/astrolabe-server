package graph

import (
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

// ResourceStatus represents the status of a Kubernetes resource
type ResourceStatus string

const (
	StatusReady   ResourceStatus = "Ready"
	StatusError   ResourceStatus = "Error"
	StatusPending ResourceStatus = "Pending"
	StatusUnknown ResourceStatus = "Unknown"
)

// Node represents a Kubernetes resource in the graph
type Node struct {
	UID               types.UID         `json:"uid"`
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	Kind              string            `json:"kind"`
	APIVersion        string            `json:"apiVersion"`
	ResourceVersion   string            `json:"resourceVersion"`
	Labels            map[string]string `json:"labels"`
	Annotations       map[string]string `json:"annotations"`
	CreationTimestamp time.Time         `json:"creationTimestamp"`
	Status            ResourceStatus    `json:"status"`
	StatusMessage     string            `json:"statusMessage"`

	// Helm-specific fields
	HelmChart   string `json:"helmChart,omitempty"`
	HelmRelease string `json:"helmRelease,omitempty"`

	// Resource-specific metadata
	Metadata *ResourceMetadata `json:"metadata,omitempty"`

	// Graph edges (stored as UIDs for efficient lookups)
	OutgoingEdges map[types.UID]*Edge `json:"-"` // Edges from this node
	IncomingEdges map[types.UID]*Edge `json:"-"` // Edges to this node
}

// ResourceMetadata contains resource-specific metadata
type ResourceMetadata struct {
	// Pod-specific
	NodeName     string `json:"nodeName,omitempty"`
	Image        string `json:"image,omitempty"`
	RestartCount int    `json:"restartCount,omitempty"`

	// Workload-specific (Deployment, StatefulSet, etc.)
	Replicas *ReplicaInfo `json:"replicas,omitempty"`

	// PVC-specific
	VolumeName string `json:"volumeName,omitempty"`

	// PV-specific
	ClaimRef *ObjectReference `json:"claimRef,omitempty"`

	// Service-specific
	ClusterIP   string `json:"clusterIP,omitempty"`
	ServiceType string `json:"serviceType,omitempty"`

	// Ingress-specific
	IngressClass string `json:"ingressClass,omitempty"`

	// HPA-specific
	ScaleTargetRef  *ObjectReference `json:"scaleTargetRef,omitempty"`
	MinReplicas     *int32           `json:"minReplicas,omitempty"`
	MaxReplicas     int32            `json:"maxReplicas,omitempty"`
	CurrentReplicas int32            `json:"currentReplicas,omitempty"`
	DesiredReplicas int32            `json:"desiredReplicas,omitempty"`
}

// ReplicaInfo contains replica information for workload resources
type ReplicaInfo struct {
	Desired   int32 `json:"desired"`
	Current   int32 `json:"current"`
	Ready     int32 `json:"ready"`
	Available int32 `json:"available"`
}

// ObjectReference is a simplified reference to another object
type ObjectReference struct {
	Kind      string    `json:"kind"`
	Namespace string    `json:"namespace,omitempty"`
	Name      string    `json:"name"`
	UID       types.UID `json:"uid,omitempty"`
}

// EdgeType represents the type of relationship between resources
type EdgeType string

const (
	// Ownership edges
	EdgeOwnership EdgeType = "owns" // Deployment -> ReplicaSet -> Pod

	// Service edges
	EdgeServiceSelector EdgeType = "selects"   // Service -> Pod (via selector)
	EdgeServiceEndpoint EdgeType = "endpoints" // Service -> EndpointSlice

	// Ingress edges
	EdgeIngressBackend EdgeType = "routes-to" // Ingress -> Service

	// Volume edges
	EdgePodVolume  EdgeType = "mounts" // Pod -> PVC
	EdgePVCBinding EdgeType = "binds"  // PVC -> PV

	// ConfigMap/Secret edges
	EdgeConfigMapRef EdgeType = "uses-configmap" // Pod/Workload -> ConfigMap
	EdgeSecretRef    EdgeType = "uses-secret"    // Pod/Workload -> Secret

	// ServiceAccount edges
	EdgeServiceAccount EdgeType = "uses-sa" // Pod/Workload -> ServiceAccount

	// HPA edges
	EdgeHPATarget EdgeType = "scales" // HPA -> Deployment/StatefulSet
)

// Edge represents a relationship between two resources
type Edge struct {
	Type     EdgeType          `json:"type"`
	FromUID  types.UID         `json:"fromUID"`
	ToUID    types.UID         `json:"toUID"`
	Metadata map[string]string `json:"metadata,omitempty"` // Additional edge metadata
}

// Graph represents the in-memory resource graph
type Graph struct {
	mu    sync.RWMutex
	nodes map[types.UID]*Node

	// Index by namespace and kind for efficient queries
	byNamespaceKind map[string]map[string][]*Node // namespace -> kind -> nodes

	// Index by Helm release for efficient queries
	byHelmRelease map[string][]*Node // release name -> nodes

	// Index by labels for efficient selector queries
	byLabel map[string]map[string][]*Node // label key -> label value -> nodes
}

// NewGraph creates a new empty graph
func NewGraph() *Graph {
	return &Graph{
		nodes:           make(map[types.UID]*Node),
		byNamespaceKind: make(map[string]map[string][]*Node),
		byHelmRelease:   make(map[string][]*Node),
		byLabel:         make(map[string]map[string][]*Node),
	}
}

// AddNode adds or updates a node in the graph
func (g *Graph) AddNode(node *Node) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Check if this is an update or new node
	_, isUpdate := g.nodes[node.UID]

	// Remove old node from indexes if it exists
	if oldNode, exists := g.nodes[node.UID]; exists {
		g.removeFromIndexes(oldNode)
	}

	// Initialize edge maps if nil
	if node.OutgoingEdges == nil {
		node.OutgoingEdges = make(map[types.UID]*Edge)
	}
	if node.IncomingEdges == nil {
		node.IncomingEdges = make(map[types.UID]*Edge)
	}

	// Add to main map
	g.nodes[node.UID] = node

	// Add to indexes
	g.addToIndexes(node)

	// Log the operation
	if isUpdate {
		klog.V(3).Infof("Graph: UPDATED %s/%s (release: %s, status: %s)", node.Kind, node.Name, node.HelmRelease, node.Status)
	} else {
		klog.V(2).Infof("Graph: ADDED %s/%s (release: %s, status: %s)", node.Kind, node.Name, node.HelmRelease, node.Status)
	}
}

// RemoveNode removes a node and its edges from the graph
func (g *Graph) RemoveNode(uid types.UID) {
	g.mu.Lock()
	defer g.mu.Unlock()

	node, exists := g.nodes[uid]
	if !exists {
		return
	}

	// Remove all edges connected to this node
	for _, edge := range node.OutgoingEdges {
		if toNode, exists := g.nodes[edge.ToUID]; exists {
			delete(toNode.IncomingEdges, uid)
		}
	}
	for _, edge := range node.IncomingEdges {
		if fromNode, exists := g.nodes[edge.FromUID]; exists {
			delete(fromNode.OutgoingEdges, uid)
		}
	}

	// Remove from indexes
	g.removeFromIndexes(node)

	// Remove from main map
	delete(g.nodes, uid)
}

// GetNode retrieves a node by UID
func (g *Graph) GetNode(uid types.UID) (*Node, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	node, exists := g.nodes[uid]
	return node, exists
}

// AddEdge adds an edge between two nodes
func (g *Graph) AddEdge(edge *Edge) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	fromNode, fromExists := g.nodes[edge.FromUID]
	toNode, toExists := g.nodes[edge.ToUID]

	if !fromExists || !toExists {
		return false
	}

	fromNode.OutgoingEdges[edge.ToUID] = edge
	toNode.IncomingEdges[edge.FromUID] = edge

	return true
}

// RemoveEdge removes an edge between two nodes
func (g *Graph) RemoveEdge(fromUID, toUID types.UID) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if fromNode, exists := g.nodes[fromUID]; exists {
		delete(fromNode.OutgoingEdges, toUID)
	}

	if toNode, exists := g.nodes[toUID]; exists {
		delete(toNode.IncomingEdges, fromUID)
	}
}

// GetNodesByNamespaceKind returns all nodes of a specific kind in a namespace
func (g *Graph) GetNodesByNamespaceKind(namespace, kind string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nsKey := namespace
	if nsKey == "" {
		nsKey = "_cluster"
	}

	if kindMap, exists := g.byNamespaceKind[nsKey]; exists {
		if nodes, exists := kindMap[kind]; exists {
			// Return a copy to avoid concurrent modification
			result := make([]*Node, len(nodes))
			copy(result, nodes)
			return result
		}
	}
	return nil
}

// GetNodesByHelmRelease returns all nodes belonging to a Helm release
func (g *Graph) GetNodesByHelmRelease(release string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if nodes, exists := g.byHelmRelease[release]; exists {
		// Return a copy to avoid concurrent modification
		result := make([]*Node, len(nodes))
		copy(result, nodes)
		return result
	}
	return nil
}

// GetNodesByLabelSelector returns nodes matching a label selector
func (g *Graph) GetNodesByLabelSelector(selector map[string]string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(selector) == 0 {
		return nil
	}

	// Start with nodes matching the first label
	var candidates []*Node
	first := true

	for key, value := range selector {
		if valueMap, exists := g.byLabel[key]; exists {
			if nodes, exists := valueMap[value]; exists {
				if first {
					candidates = make([]*Node, len(nodes))
					copy(candidates, nodes)
					first = false
				} else {
					// Intersect with existing candidates
					candidates = g.intersectNodes(candidates, nodes)
				}
			} else {
				return nil // No nodes match this label
			}
		} else {
			return nil // No nodes have this label key
		}
	}

	return candidates
}

// GetAllNodes returns all nodes in the graph
func (g *Graph) GetAllNodes() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make([]*Node, 0, len(g.nodes))
	for _, node := range g.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// GetAllHelmReleases returns all unique Helm release names
func (g *Graph) GetAllHelmReleases() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	releases := make([]string, 0, len(g.byHelmRelease))
	for release := range g.byHelmRelease {
		if release != "" {
			releases = append(releases, release)
		}
	}
	return releases
}

// GetAllHelmCharts returns all unique Helm chart names
func (g *Graph) GetAllHelmCharts() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	charts := make(map[string]bool)
	for _, node := range g.nodes {
		if node.HelmChart != "" {
			charts[node.HelmChart] = true
		}
	}

	result := make([]string, 0, len(charts))
	for chart := range charts {
		result = append(result, chart)
	}
	return result
}

// Helper functions

func (g *Graph) addToIndexes(node *Node) {
	// Add to namespace/kind index
	nsKey := node.Namespace
	if nsKey == "" {
		nsKey = "_cluster" // For cluster-scoped resources
	}

	if _, exists := g.byNamespaceKind[nsKey]; !exists {
		g.byNamespaceKind[nsKey] = make(map[string][]*Node)
	}
	g.byNamespaceKind[nsKey][node.Kind] = append(g.byNamespaceKind[nsKey][node.Kind], node)

	// Add to Helm release index
	if node.HelmRelease != "" {
		g.byHelmRelease[node.HelmRelease] = append(g.byHelmRelease[node.HelmRelease], node)
	}

	// Add to label index
	for key, value := range node.Labels {
		if _, exists := g.byLabel[key]; !exists {
			g.byLabel[key] = make(map[string][]*Node)
		}
		g.byLabel[key][value] = append(g.byLabel[key][value], node)
	}
}

func (g *Graph) removeFromIndexes(node *Node) {
	// Remove from namespace/kind index
	nsKey := node.Namespace
	if nsKey == "" {
		nsKey = "_cluster"
	}

	if kindMap, exists := g.byNamespaceKind[nsKey]; exists {
		if nodes, exists := kindMap[node.Kind]; exists {
			kindMap[node.Kind] = g.removeNodeFromSlice(nodes, node.UID)
			if len(kindMap[node.Kind]) == 0 {
				delete(kindMap, node.Kind)
			}
		}
		if len(kindMap) == 0 {
			delete(g.byNamespaceKind, nsKey)
		}
	}

	// Remove from Helm release index
	if node.HelmRelease != "" {
		if nodes, exists := g.byHelmRelease[node.HelmRelease]; exists {
			g.byHelmRelease[node.HelmRelease] = g.removeNodeFromSlice(nodes, node.UID)
			if len(g.byHelmRelease[node.HelmRelease]) == 0 {
				delete(g.byHelmRelease, node.HelmRelease)
			}
		}
	}

	// Remove from label index
	for key, value := range node.Labels {
		if valueMap, exists := g.byLabel[key]; exists {
			if nodes, exists := valueMap[value]; exists {
				valueMap[value] = g.removeNodeFromSlice(nodes, node.UID)
				if len(valueMap[value]) == 0 {
					delete(valueMap, value)
				}
			}
			if len(valueMap) == 0 {
				delete(g.byLabel, key)
			}
		}
	}
}

func (g *Graph) removeNodeFromSlice(nodes []*Node, uid types.UID) []*Node {
	for i, node := range nodes {
		if node.UID == uid {
			return append(nodes[:i], nodes[i+1:]...)
		}
	}
	return nodes
}

func (g *Graph) intersectNodes(a, b []*Node) []*Node {
	uidMap := make(map[types.UID]bool)
	for _, node := range a {
		uidMap[node.UID] = true
	}

	result := make([]*Node, 0)
	for _, node := range b {
		if uidMap[node.UID] {
			result = append(result, node)
		}
	}
	return result
}

// NewNodeFromObject creates a Node from a Kubernetes object
func NewNodeFromObject(obj metav1.Object, kind, apiVersion string) *Node {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	node := &Node{
		UID:               obj.GetUID(),
		Name:              obj.GetName(),
		Namespace:         obj.GetNamespace(),
		Kind:              kind,
		APIVersion:        apiVersion,
		ResourceVersion:   obj.GetResourceVersion(),
		Labels:            labels,
		Annotations:       annotations,
		CreationTimestamp: obj.GetCreationTimestamp().Time,
		Status:            StatusUnknown,
		OutgoingEdges:     make(map[types.UID]*Edge),
		IncomingEdges:     make(map[types.UID]*Edge),
	}

	// Extract Helm information from labels/annotations
	if chart, ok := annotations["helm.sh/chart"]; ok {
		node.HelmChart = chart
	}

	if release, ok := annotations["meta.helm.sh/release-name"]; ok {
		node.HelmRelease = release
	}

	return node
}

type GraphInterface interface {
	GetNode(uid types.UID) (*Node, bool)
	GetAllNodes() []*Node
	GetNodesByNamespaceKind(namespace, kind string) []*Node
	GetNodesByHelmRelease(release string) []*Node
	GetAllHelmReleases() []string
	GetAllHelmCharts() []string
	AddNode(node *Node)
	RemoveNode(uid types.UID)
	AddEdge(edge *Edge) bool
	RemoveEdge(fromUID, toUID types.UID)
}
