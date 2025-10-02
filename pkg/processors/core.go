package processors

import (
	"fmt"

	"github.com/ammarlakis/astrolabe/pkg/graph"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// PodProcessor processes Pod resources
type PodProcessor struct {
	*BaseProcessor
}

func NewPodProcessor(g GraphInterface) *PodProcessor {
	return &PodProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *PodProcessor) Process(obj interface{}, eventType EventType) error {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return fmt.Errorf("expected Pod, got %T", obj)
	}
	
	if eventType == EventDelete {
		return p.handleDelete(pod, "Pod")
	}
	
	node := graph.NewNodeFromObject(pod, "Pod", "v1")
	node.Status, node.StatusMessage = p.getPodStatus(pod)
	
	// Set metadata
	metadata := &graph.ResourceMetadata{
		NodeName:     pod.Spec.NodeName,
		RestartCount: p.getTotalRestartCount(pod),
	}
	
	if len(pod.Spec.Containers) > 0 {
		metadata.Image = pod.Spec.Containers[0].Image
	}
	
	node.Metadata = metadata
	
	// Add node to graph
	p.graph.AddNode(node)
	
	// Create ownership edges
	p.createOwnershipEdges(node, pod.GetOwnerReferences())
	
	// Create edges to PVCs
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			if pvcNode := p.findNodeByNamespaceKindName(pod.Namespace, "PersistentVolumeClaim", volume.PersistentVolumeClaim.ClaimName); pvcNode != nil {
				p.createEdgeIfNodeExists(node.UID, pvcNode.UID, graph.EdgePodVolume)
			}
		}
	}
	
	// Create edges to ConfigMaps and Secrets
	p.createConfigMapSecretEdges(node, &pod.Spec)
	
	// Create edge to ServiceAccount
	if pod.Spec.ServiceAccountName != "" {
		if saNode := p.findNodeByNamespaceKindName(pod.Namespace, "ServiceAccount", pod.Spec.ServiceAccountName); saNode != nil {
			p.createEdgeIfNodeExists(node.UID, saNode.UID, graph.EdgeServiceAccount)
		}
	}
	
	return nil
}

func (p *PodProcessor) getPodStatus(pod *corev1.Pod) (graph.ResourceStatus, string) {
	switch pod.Status.Phase {
	case corev1.PodRunning:
		// Check container statuses
		for _, cs := range pod.Status.ContainerStatuses {
			if !cs.Ready {
				if cs.State.Waiting != nil {
					return graph.StatusPending, fmt.Sprintf("Container not ready: %s", cs.State.Waiting.Reason)
				}
				if cs.State.Terminated != nil {
					return graph.StatusError, fmt.Sprintf("Container terminated: %s", cs.State.Terminated.Reason)
				}
			}
		}
		return graph.StatusReady, "Pod is running"
	case corev1.PodPending:
		return graph.StatusPending, "Pod is pending"
	case corev1.PodSucceeded:
		return graph.StatusReady, "Pod succeeded"
	case corev1.PodFailed:
		return graph.StatusError, "Pod failed"
	case corev1.PodUnknown:
		return graph.StatusUnknown, "Pod status unknown"
	default:
		return graph.StatusUnknown, fmt.Sprintf("Unknown phase: %s", pod.Status.Phase)
	}
}

func (p *PodProcessor) getTotalRestartCount(pod *corev1.Pod) int {
	total := 0
	for _, cs := range pod.Status.ContainerStatuses {
		total += int(cs.RestartCount)
	}
	return total
}

// ServiceProcessor processes Service resources
type ServiceProcessor struct {
	*BaseProcessor
}

func NewServiceProcessor(g GraphInterface) *ServiceProcessor {
	return &ServiceProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *ServiceProcessor) Process(obj interface{}, eventType EventType) error {
	service, ok := obj.(*corev1.Service)
	if !ok {
		return fmt.Errorf("expected Service, got %T", obj)
	}
	
	if eventType == EventDelete {
		return p.handleDelete(service, "Service")
	}
	
	node := graph.NewNodeFromObject(service, "Service", "v1")
	node.Status = graph.StatusReady
	node.StatusMessage = "Service is active"
	
	node.Metadata = &graph.ResourceMetadata{
		ClusterIP:   service.Spec.ClusterIP,
		ServiceType: string(service.Spec.Type),
	}
	
	p.graph.AddNode(node)
	p.createOwnershipEdges(node, service.GetOwnerReferences())
	
	// Create edges to Pods via selector
	if len(service.Spec.Selector) > 0 {
		pods := p.findNodesByLabelSelector(service.Namespace, "Pod", service.Spec.Selector)
		for _, pod := range pods {
			p.createEdgeIfNodeExists(node.UID, pod.UID, graph.EdgeServiceSelector)
		}
	}
	
	return nil
}

// ServiceAccountProcessor processes ServiceAccount resources
type ServiceAccountProcessor struct {
	*BaseProcessor
}

func NewServiceAccountProcessor(g GraphInterface) *ServiceAccountProcessor {
	return &ServiceAccountProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *ServiceAccountProcessor) Process(obj interface{}, eventType EventType) error {
	sa, ok := obj.(*corev1.ServiceAccount)
	if !ok {
		return fmt.Errorf("expected ServiceAccount, got %T", obj)
	}
	
	if eventType == EventDelete {
		return p.handleDelete(sa, "ServiceAccount")
	}
	
	node := graph.NewNodeFromObject(sa, "ServiceAccount", "v1")
	node.Status = graph.StatusReady
	node.StatusMessage = "ServiceAccount exists"
	
	p.graph.AddNode(node)
	p.createOwnershipEdges(node, sa.GetOwnerReferences())
	
	return nil
}

// ConfigMapProcessor processes ConfigMap resources
type ConfigMapProcessor struct {
	*BaseProcessor
}

func NewConfigMapProcessor(g GraphInterface) *ConfigMapProcessor {
	return &ConfigMapProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *ConfigMapProcessor) Process(obj interface{}, eventType EventType) error {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return fmt.Errorf("expected ConfigMap, got %T", obj)
	}
	
	if eventType == EventDelete {
		return p.handleDelete(cm, "ConfigMap")
	}
	
	node := graph.NewNodeFromObject(cm, "ConfigMap", "v1")
	node.Status = graph.StatusReady
	node.StatusMessage = "ConfigMap exists"
	
	p.graph.AddNode(node)
	p.createOwnershipEdges(node, cm.GetOwnerReferences())
	
	return nil
}

// SecretProcessor processes Secret resources
type SecretProcessor struct {
	*BaseProcessor
}

func NewSecretProcessor(g GraphInterface) *SecretProcessor {
	return &SecretProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *SecretProcessor) Process(obj interface{}, eventType EventType) error {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return fmt.Errorf("expected Secret, got %T", obj)
	}
	
	if eventType == EventDelete {
		return p.handleDelete(secret, "Secret")
	}
	
	node := graph.NewNodeFromObject(secret, "Secret", "v1")
	node.Status = graph.StatusReady
	node.StatusMessage = "Secret exists"
	
	// Check if this is a Helm release secret
	if secret.Type == "helm.sh/release.v1" {
		klog.V(3).Infof("Processing Helm release secret: %s/%s", secret.Namespace, secret.Name)
		// Extract release name from secret name (format: sh.helm.release.v1.<release-name>.v<version>)
		// We can parse this if needed for better Helm integration
	}
	
	p.graph.AddNode(node)
	p.createOwnershipEdges(node, secret.GetOwnerReferences())
	
	return nil
}

// PVCProcessor processes PersistentVolumeClaim resources
type PVCProcessor struct {
	*BaseProcessor
}

func NewPVCProcessor(g GraphInterface) *PVCProcessor {
	return &PVCProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *PVCProcessor) Process(obj interface{}, eventType EventType) error {
	pvc, ok := obj.(*corev1.PersistentVolumeClaim)
	if !ok {
		return fmt.Errorf("expected PersistentVolumeClaim, got %T", obj)
	}
	
	if eventType == EventDelete {
		return p.handleDelete(pvc, "PersistentVolumeClaim")
	}
	
	node := graph.NewNodeFromObject(pvc, "PersistentVolumeClaim", "v1")
	node.Status, node.StatusMessage = p.getPVCStatus(pvc)
	
	node.Metadata = &graph.ResourceMetadata{
		VolumeName: pvc.Spec.VolumeName,
	}
	
	p.graph.AddNode(node)
	p.createOwnershipEdges(node, pvc.GetOwnerReferences())
	
	// Create edge to PV if bound
	if pvc.Spec.VolumeName != "" {
		if pvNode := p.findNodeByNamespaceKindName("", "PersistentVolume", pvc.Spec.VolumeName); pvNode != nil {
			p.createEdgeIfNodeExists(node.UID, pvNode.UID, graph.EdgePVCBinding)
		}
	}
	
	return nil
}

func (p *PVCProcessor) getPVCStatus(pvc *corev1.PersistentVolumeClaim) (graph.ResourceStatus, string) {
	switch pvc.Status.Phase {
	case corev1.ClaimBound:
		return graph.StatusReady, "Bound"
	case corev1.ClaimPending:
		return graph.StatusPending, "Pending"
	case corev1.ClaimLost:
		return graph.StatusError, "Lost"
	default:
		return graph.StatusUnknown, fmt.Sprintf("Phase: %s", pvc.Status.Phase)
	}
}

// PVProcessor processes PersistentVolume resources
type PVProcessor struct {
	*BaseProcessor
}

func NewPVProcessor(g GraphInterface) *PVProcessor {
	return &PVProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *PVProcessor) Process(obj interface{}, eventType EventType) error {
	pv, ok := obj.(*corev1.PersistentVolume)
	if !ok {
		return fmt.Errorf("expected PersistentVolume, got %T", obj)
	}
	
	if eventType == EventDelete {
		return p.handleDelete(pv, "PersistentVolume")
	}
	
	node := graph.NewNodeFromObject(pv, "PersistentVolume", "v1")
	node.Status, node.StatusMessage = p.getPVStatus(pv)
	
	// Set claim reference if bound
	if pv.Spec.ClaimRef != nil {
		node.Metadata = &graph.ResourceMetadata{
			ClaimRef: &graph.ObjectReference{
				Kind:      "PersistentVolumeClaim",
				Namespace: pv.Spec.ClaimRef.Namespace,
				Name:      pv.Spec.ClaimRef.Name,
				UID:       pv.Spec.ClaimRef.UID,
			},
		}
	}
	
	p.graph.AddNode(node)
	p.createOwnershipEdges(node, pv.GetOwnerReferences())
	
	return nil
}

func (p *PVProcessor) getPVStatus(pv *corev1.PersistentVolume) (graph.ResourceStatus, string) {
	switch pv.Status.Phase {
	case corev1.VolumeBound:
		return graph.StatusReady, "Bound"
	case corev1.VolumeAvailable:
		return graph.StatusReady, "Available"
	case corev1.VolumeReleased:
		return graph.StatusPending, "Released"
	case corev1.VolumeFailed:
		return graph.StatusError, "Failed"
	default:
		return graph.StatusUnknown, fmt.Sprintf("Phase: %s", pv.Status.Phase)
	}
}

// NamespaceProcessor processes Namespace resources
type NamespaceProcessor struct {
	*BaseProcessor
}

func NewNamespaceProcessor(g GraphInterface) *NamespaceProcessor {
	return &NamespaceProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *NamespaceProcessor) Process(obj interface{}, eventType EventType) error {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return fmt.Errorf("expected Namespace, got %T", obj)
	}
	
	if eventType == EventDelete {
		return p.handleDelete(ns, "Namespace")
	}
	
	node := graph.NewNodeFromObject(ns, "Namespace", "v1")
	
	switch ns.Status.Phase {
	case corev1.NamespaceActive:
		node.Status = graph.StatusReady
		node.StatusMessage = "Active"
	case corev1.NamespaceTerminating:
		node.Status = graph.StatusPending
		node.StatusMessage = "Terminating"
	default:
		node.Status = graph.StatusUnknown
		node.StatusMessage = fmt.Sprintf("Phase: %s", ns.Status.Phase)
	}
	
	p.graph.AddNode(node)
	
	return nil
}

// createConfigMapSecretEdges creates edges from a pod spec to ConfigMaps and Secrets
func (p *BaseProcessor) createConfigMapSecretEdges(node *graph.Node, podSpec *corev1.PodSpec) {
	// From volumes
	for _, volume := range podSpec.Volumes {
		if volume.ConfigMap != nil {
			if cmNode := p.findNodeByNamespaceKindName(node.Namespace, "ConfigMap", volume.ConfigMap.Name); cmNode != nil {
				p.createEdgeIfNodeExists(node.UID, cmNode.UID, graph.EdgeConfigMapRef)
			}
		}
		if volume.Secret != nil {
			if secretNode := p.findNodeByNamespaceKindName(node.Namespace, "Secret", volume.Secret.SecretName); secretNode != nil {
				p.createEdgeIfNodeExists(node.UID, secretNode.UID, graph.EdgeSecretRef)
			}
		}
	}
	
	// From containers
	for _, container := range podSpec.Containers {
		// From envFrom
		for _, envFrom := range container.EnvFrom {
			if envFrom.ConfigMapRef != nil {
				if cmNode := p.findNodeByNamespaceKindName(node.Namespace, "ConfigMap", envFrom.ConfigMapRef.Name); cmNode != nil {
					p.createEdgeIfNodeExists(node.UID, cmNode.UID, graph.EdgeConfigMapRef)
				}
			}
			if envFrom.SecretRef != nil {
				if secretNode := p.findNodeByNamespaceKindName(node.Namespace, "Secret", envFrom.SecretRef.Name); secretNode != nil {
					p.createEdgeIfNodeExists(node.UID, secretNode.UID, graph.EdgeSecretRef)
				}
			}
		}
		
		// From env
		for _, env := range container.Env {
			if env.ValueFrom != nil {
				if env.ValueFrom.ConfigMapKeyRef != nil {
					if cmNode := p.findNodeByNamespaceKindName(node.Namespace, "ConfigMap", env.ValueFrom.ConfigMapKeyRef.Name); cmNode != nil {
						p.createEdgeIfNodeExists(node.UID, cmNode.UID, graph.EdgeConfigMapRef)
					}
				}
				if env.ValueFrom.SecretKeyRef != nil {
					if secretNode := p.findNodeByNamespaceKindName(node.Namespace, "Secret", env.ValueFrom.SecretKeyRef.Name); secretNode != nil {
						p.createEdgeIfNodeExists(node.UID, secretNode.UID, graph.EdgeSecretRef)
					}
				}
			}
		}
	}
}
