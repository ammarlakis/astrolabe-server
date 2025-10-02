package processors

import (
	"fmt"

	"github.com/ammarlakis/astrolabe/pkg/graph"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

// BaseProcessor provides common functionality for all processors
type BaseProcessor struct {
	graph GraphInterface
}

// NewBaseProcessor creates a new base processor
func NewBaseProcessor(g GraphInterface) *BaseProcessor {
	return &BaseProcessor{graph: g}
}

// handleDelete removes a node from the graph
func (p *BaseProcessor) handleDelete(obj interface{}, kind string) error {
	metaObj, ok := obj.(v1.Object)
	if !ok {
		return fmt.Errorf("object does not implement metav1.Object")
	}
	
	uid := metaObj.GetUID()
	klog.V(3).Infof("Deleting %s: %s/%s (UID: %s)", kind, metaObj.GetNamespace(), metaObj.GetName(), uid)
	
	p.graph.RemoveNode(uid)
	return nil
}

// createOwnershipEdges creates edges from owner references
func (p *BaseProcessor) createOwnershipEdges(node *graph.Node, ownerRefs []v1.OwnerReference) {
	for _, owner := range ownerRefs {
		// Try to find the owner node in the graph
		if ownerNode, exists := p.graph.GetNode(owner.UID); exists {
			edge := &graph.Edge{
				Type:    graph.EdgeOwnership,
				FromUID: owner.UID,
				ToUID:   node.UID,
			}
			p.graph.AddEdge(edge)
			klog.V(4).Infof("Created ownership edge: %s/%s -> %s/%s", 
				ownerNode.Kind, ownerNode.Name, node.Kind, node.Name)
		} else {
			klog.V(4).Infof("Owner not found in graph yet: %s/%s (UID: %s)", 
				owner.Kind, owner.Name, owner.UID)
		}
	}
}

// findNodeByNamespaceKindName finds a node by namespace, kind, and name
func (p *BaseProcessor) findNodeByNamespaceKindName(namespace, kind, name string) *graph.Node {
	nodes := p.graph.GetNodesByNamespaceKind(namespace, kind)
	for _, node := range nodes {
		if node.Name == name {
			return node
		}
	}
	return nil
}

// findNodesByLabelSelector finds nodes matching a label selector
func (p *BaseProcessor) findNodesByLabelSelector(namespace, kind string, selector map[string]string) []*graph.Node {
	// Get all nodes of the specified kind in the namespace
	nodes := p.graph.GetNodesByNamespaceKind(namespace, kind)
	
	if len(selector) == 0 {
		return nodes
	}
	
	// Filter by selector
	var result []*graph.Node
	for _, node := range nodes {
		if matchesSelector(node.Labels, selector) {
			result = append(result, node)
		}
	}
	return result
}

// matchesSelector checks if labels match a selector
func matchesSelector(labels, selector map[string]string) bool {
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

// createEdgeIfNodeExists creates an edge if the target node exists
func (p *BaseProcessor) createEdgeIfNodeExists(fromUID, toUID types.UID, edgeType graph.EdgeType) bool {
	edge := &graph.Edge{
		Type:    edgeType,
		FromUID: fromUID,
		ToUID:   toUID,
	}
	return p.graph.AddEdge(edge)
}
