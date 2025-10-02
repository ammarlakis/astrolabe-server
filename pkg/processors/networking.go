package processors

import (
	"fmt"

	"github.com/ammarlakis/astrolabe/pkg/graph"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/klog/v2"
)

// IngressProcessor processes Ingress resources
type IngressProcessor struct {
	*BaseProcessor
}

func NewIngressProcessor(g GraphInterface) *IngressProcessor {
	return &IngressProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *IngressProcessor) Process(obj interface{}, eventType EventType) error {
	ingress, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return fmt.Errorf("expected Ingress, got %T", obj)
	}
	
	if eventType == EventDelete {
		return p.handleDelete(ingress, "Ingress")
	}
	
	node := graph.NewNodeFromObject(ingress, "Ingress", "networking.k8s.io/v1")
	
	// Check if ingress has load balancer IP
	if len(ingress.Status.LoadBalancer.Ingress) > 0 {
		node.Status = graph.StatusReady
		node.StatusMessage = "Ingress has load balancer"
	} else {
		node.Status = graph.StatusPending
		node.StatusMessage = "Waiting for load balancer"
	}
	
	// Set ingress class
	if ingress.Spec.IngressClassName != nil {
		node.Metadata = &graph.ResourceMetadata{
			IngressClass: *ingress.Spec.IngressClassName,
		}
	}
	
	p.graph.AddNode(node)
	p.createOwnershipEdges(node, ingress.GetOwnerReferences())
	
	// Create edges to Services
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP != nil {
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service != nil {
					if svcNode := p.findNodeByNamespaceKindName(ingress.Namespace, "Service", path.Backend.Service.Name); svcNode != nil {
						p.createEdgeIfNodeExists(node.UID, svcNode.UID, graph.EdgeIngressBackend)
					}
				}
			}
		}
	}
	
	// Handle default backend
	if ingress.Spec.DefaultBackend != nil && ingress.Spec.DefaultBackend.Service != nil {
		if svcNode := p.findNodeByNamespaceKindName(ingress.Namespace, "Service", ingress.Spec.DefaultBackend.Service.Name); svcNode != nil {
			p.createEdgeIfNodeExists(node.UID, svcNode.UID, graph.EdgeIngressBackend)
		}
	}
	
	return nil
}

// EndpointSliceProcessor processes EndpointSlice resources
type EndpointSliceProcessor struct {
	*BaseProcessor
}

func NewEndpointSliceProcessor(g GraphInterface) *EndpointSliceProcessor {
	return &EndpointSliceProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *EndpointSliceProcessor) Process(obj interface{}, eventType EventType) error {
	endpointSlice, ok := obj.(*discoveryv1.EndpointSlice)
	if !ok {
		return fmt.Errorf("expected EndpointSlice, got %T", obj)
	}
	
	if eventType == EventDelete {
		return p.handleDelete(endpointSlice, "EndpointSlice")
	}
	
	node := graph.NewNodeFromObject(endpointSlice, "EndpointSlice", "discovery.k8s.io/v1")
	
	// Count ready endpoints
	readyCount := 0
	for _, endpoint := range endpointSlice.Endpoints {
		if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
			readyCount++
		}
	}
	
	if readyCount > 0 {
		node.Status = graph.StatusReady
		node.StatusMessage = fmt.Sprintf("%d ready endpoint(s)", readyCount)
	} else {
		node.Status = graph.StatusPending
		node.StatusMessage = "No ready endpoints"
	}
	
	p.graph.AddNode(node)
	p.createOwnershipEdges(node, endpointSlice.GetOwnerReferences())
	
	// Create edge to Service (via kubernetes.io/service-name label)
	if serviceName, ok := endpointSlice.Labels["kubernetes.io/service-name"]; ok {
		if svcNode := p.findNodeByNamespaceKindName(endpointSlice.Namespace, "Service", serviceName); svcNode != nil {
			// Edge from Service to EndpointSlice
			p.createEdgeIfNodeExists(svcNode.UID, node.UID, graph.EdgeServiceEndpoint)
		}
	}
	
	// Create edges to Pods
	for _, endpoint := range endpointSlice.Endpoints {
		if endpoint.TargetRef != nil && endpoint.TargetRef.Kind == "Pod" {
			if podNode := p.findNodeByNamespaceKindName(endpointSlice.Namespace, "Pod", endpoint.TargetRef.Name); podNode != nil {
				// Edge from EndpointSlice to Pod
				p.createEdgeIfNodeExists(node.UID, podNode.UID, graph.EdgeServiceSelector)
			}
		}
	}
	
	return nil
}

// StorageClassProcessor processes StorageClass resources
type StorageClassProcessor struct {
	*BaseProcessor
}

func NewStorageClassProcessor(g GraphInterface) *StorageClassProcessor {
	return &StorageClassProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *StorageClassProcessor) Process(obj interface{}, eventType EventType) error {
	sc, ok := obj.(*storagev1.StorageClass)
	if !ok {
		return fmt.Errorf("expected StorageClass, got %T", obj)
	}
	
	if eventType == EventDelete {
		return p.handleDelete(sc, "StorageClass")
	}
	
	node := graph.NewNodeFromObject(sc, "StorageClass", "storage.k8s.io/v1")
	node.Status = graph.StatusReady
	node.StatusMessage = "StorageClass exists"
	
	p.graph.AddNode(node)
	
	return nil
}

// HPAProcessor processes HorizontalPodAutoscaler resources
type HPAProcessor struct {
	*BaseProcessor
}

func NewHPAProcessor(g GraphInterface) *HPAProcessor {
	return &HPAProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *HPAProcessor) Process(obj interface{}, eventType EventType) error {
	hpa, ok := obj.(*autoscalingv2.HorizontalPodAutoscaler)
	if !ok {
		return fmt.Errorf("expected HorizontalPodAutoscaler, got %T", obj)
	}
	
	if eventType == EventDelete {
		return p.handleDelete(hpa, "HorizontalPodAutoscaler")
	}
	
	node := graph.NewNodeFromObject(hpa, "HorizontalPodAutoscaler", "autoscaling/v2")
	
	// Check HPA status
	ableToScale := false
	for _, condition := range hpa.Status.Conditions {
		if condition.Type == autoscalingv2.AbleToScale && condition.Status == "True" {
			ableToScale = true
			break
		}
	}
	
	if ableToScale {
		node.Status = graph.StatusReady
		node.StatusMessage = fmt.Sprintf("Scaling: %d/%d replicas", hpa.Status.CurrentReplicas, hpa.Status.DesiredReplicas)
	} else {
		node.Status = graph.StatusPending
		node.StatusMessage = "Unable to scale"
	}
	
	// Set metadata
	node.Metadata = &graph.ResourceMetadata{
		ScaleTargetRef: &graph.ObjectReference{
			Kind: hpa.Spec.ScaleTargetRef.Kind,
			Name: hpa.Spec.ScaleTargetRef.Name,
		},
		MinReplicas:     hpa.Spec.MinReplicas,
		MaxReplicas:     hpa.Spec.MaxReplicas,
		CurrentReplicas: hpa.Status.CurrentReplicas,
		DesiredReplicas: hpa.Status.DesiredReplicas,
	}
	
	p.graph.AddNode(node)
	p.createOwnershipEdges(node, hpa.GetOwnerReferences())
	
	// Create edge to scale target
	targetNode := p.findNodeByNamespaceKindName(hpa.Namespace, hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name)
	if targetNode != nil {
		p.createEdgeIfNodeExists(node.UID, targetNode.UID, graph.EdgeHPATarget)
		klog.V(4).Infof("Created HPA edge: %s/%s -> %s/%s", 
			node.Kind, node.Name, targetNode.Kind, targetNode.Name)
	}
	
	return nil
}

// PDBProcessor processes PodDisruptionBudget resources
type PDBProcessor struct {
	*BaseProcessor
}

func NewPDBProcessor(g GraphInterface) *PDBProcessor {
	return &PDBProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *PDBProcessor) Process(obj interface{}, eventType EventType) error {
	pdb, ok := obj.(*policyv1.PodDisruptionBudget)
	if !ok {
		return fmt.Errorf("expected PodDisruptionBudget, got %T", obj)
	}
	
	if eventType == EventDelete {
		return p.handleDelete(pdb, "PodDisruptionBudget")
	}
	
	node := graph.NewNodeFromObject(pdb, "PodDisruptionBudget", "policy/v1")
	
	// Check PDB status
	if pdb.Status.CurrentHealthy >= pdb.Status.DesiredHealthy {
		node.Status = graph.StatusReady
		node.StatusMessage = fmt.Sprintf("Healthy: %d/%d", pdb.Status.CurrentHealthy, pdb.Status.DesiredHealthy)
	} else {
		node.Status = graph.StatusPending
		node.StatusMessage = fmt.Sprintf("Unhealthy: %d/%d", pdb.Status.CurrentHealthy, pdb.Status.DesiredHealthy)
	}
	
	p.graph.AddNode(node)
	p.createOwnershipEdges(node, pdb.GetOwnerReferences())
	
	// Create edges to Pods via selector
	if pdb.Spec.Selector != nil {
		pods := p.findNodesByLabelSelector(pdb.Namespace, "Pod", pdb.Spec.Selector.MatchLabels)
		for _, pod := range pods {
			p.createEdgeIfNodeExists(node.UID, pod.UID, graph.EdgeServiceSelector)
		}
	}
	
	return nil
}
