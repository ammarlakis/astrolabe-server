package informers

import (
	"context"
	"fmt"
	"time"

	"github.com/ammarlakis/astrolabe/pkg/graph"
	"github.com/ammarlakis/astrolabe/pkg/processors"
	
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	defaultResyncPeriod = 10 * time.Minute
)

// GraphInterface defines the interface for graph operations
type GraphInterface interface {
	AddNode(node *graph.Node)
	RemoveNode(uid types.UID)
	GetNode(uid types.UID) (*graph.Node, bool)
	AddEdge(edge *graph.Edge) bool
	RemoveEdge(fromUID, toUID types.UID)
	GetAllNodes() []*graph.Node
	GetNodesByNamespaceKind(namespace, kind string) []*graph.Node
	GetNodesByHelmRelease(release string) []*graph.Node
	GetAllHelmReleases() []string
	GetAllHelmCharts() []string
}

// Manager manages all Kubernetes informers and updates the graph
type Manager struct {
	clientset       *kubernetes.Clientset
	graph           GraphInterface
	factory         informers.SharedInformerFactory
	stopCh          chan struct{}
	labelSelector   string
	
	// Processors for different resource types
	processors      *processors.ProcessorRegistry
}

// NewManager creates a new informer manager
func NewManager(clientset *kubernetes.Clientset, g GraphInterface, labelSelector string) *Manager {
	// Create shared informer factory with label selector
	var factory informers.SharedInformerFactory
	
	if labelSelector != "" {
		factory = informers.NewSharedInformerFactoryWithOptions(
			clientset,
			defaultResyncPeriod,
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = labelSelector
			}),
		)
	} else {
		factory = informers.NewSharedInformerFactory(clientset, defaultResyncPeriod)
	}
	
	return &Manager{
		clientset:     clientset,
		graph:         g,
		factory:       factory,
		stopCh:        make(chan struct{}),
		labelSelector: labelSelector,
		processors:    processors.NewProcessorRegistry(g),
	}
}

// Start starts all informers
func (m *Manager) Start(ctx context.Context) error {
	klog.Info("Starting informer manager")
	
	// Register all informers
	if err := m.registerInformers(); err != nil {
		return fmt.Errorf("failed to register informers: %w", err)
	}
	
	// Start the factory
	m.factory.Start(m.stopCh)
	
	// Wait for caches to sync
	klog.Info("Waiting for informer caches to sync")
	if !m.waitForCacheSync() {
		return fmt.Errorf("failed to sync informer caches")
	}
	
	klog.Info("All informer caches synced successfully")
	
	// Wait for context cancellation
	<-ctx.Done()
	m.Stop()
	
	return nil
}

// Stop stops all informers
func (m *Manager) Stop() {
	klog.Info("Stopping informer manager")
	close(m.stopCh)
}

// registerInformers registers all resource informers
func (m *Manager) registerInformers() error {
	// Core resources
	m.registerPodInformer()
	m.registerServiceInformer()
	m.registerServiceAccountInformer()
	m.registerConfigMapInformer()
	m.registerSecretInformer()
	m.registerPVCInformer()
	m.registerPVInformer()
	m.registerNamespaceInformer()
	
	// Apps resources
	m.registerDeploymentInformer()
	m.registerStatefulSetInformer()
	m.registerDaemonSetInformer()
	m.registerReplicaSetInformer()
	
	// Batch resources
	m.registerJobInformer()
	m.registerCronJobInformer()
	
	// Networking resources
	m.registerIngressInformer()
	m.registerEndpointSliceInformer()
	
	// Storage resources
	m.registerStorageClassInformer()
	
	// Autoscaling resources
	m.registerHPAInformer()
	
	// Policy resources
	m.registerPDBInformer()
	
	return nil
}

// waitForCacheSync waits for all informer caches to sync
func (m *Manager) waitForCacheSync() bool {
	synced := m.factory.WaitForCacheSync(m.stopCh)
	for informerType, ok := range synced {
		if !ok {
			klog.Errorf("Failed to sync cache for %v", informerType)
			return false
		}
	}
	return true
}

// Generic event handlers

func (m *Manager) onAdd(obj interface{}, kind string) {
	klog.V(2).Infof("Cache: ADD %s", kind)
	m.processors.Process(obj, kind, processors.EventAdd)
}

func (m *Manager) onUpdate(oldObj, newObj interface{}, kind string) {
	klog.V(2).Infof("Cache: UPDATE %s", kind)
	m.processors.Process(newObj, kind, processors.EventUpdate)
}

func (m *Manager) onDelete(obj interface{}, kind string) {
	klog.V(2).Infof("Cache: DELETE %s", kind)
	m.processors.Process(obj, kind, processors.EventDelete)
}

// Resource-specific informer registrations

func (m *Manager) registerPodInformer() {
	informer := m.factory.Core().V1().Pods().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "Pod")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "Pod")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "Pod")
		},
	})
}

func (m *Manager) registerServiceInformer() {
	informer := m.factory.Core().V1().Services().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "Service")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "Service")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "Service")
		},
	})
}

func (m *Manager) registerServiceAccountInformer() {
	informer := m.factory.Core().V1().ServiceAccounts().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "ServiceAccount")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "ServiceAccount")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "ServiceAccount")
		},
	})
}

func (m *Manager) registerConfigMapInformer() {
	informer := m.factory.Core().V1().ConfigMaps().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "ConfigMap")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "ConfigMap")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "ConfigMap")
		},
	})
}

func (m *Manager) registerSecretInformer() {
	informer := m.factory.Core().V1().Secrets().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// Check if this is a Helm release secret
			if secret, ok := obj.(*corev1.Secret); ok {
				if secret.Type == "helm.sh/release.v1" {
					klog.V(3).Infof("Detected Helm release secret: %s/%s", secret.Namespace, secret.Name)
				}
			}
			m.onAdd(obj, "Secret")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "Secret")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "Secret")
		},
	})
}

func (m *Manager) registerPVCInformer() {
	informer := m.factory.Core().V1().PersistentVolumeClaims().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "PersistentVolumeClaim")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "PersistentVolumeClaim")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "PersistentVolumeClaim")
		},
	})
}

func (m *Manager) registerPVInformer() {
	informer := m.factory.Core().V1().PersistentVolumes().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "PersistentVolume")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "PersistentVolume")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "PersistentVolume")
		},
	})
}

func (m *Manager) registerNamespaceInformer() {
	informer := m.factory.Core().V1().Namespaces().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "Namespace")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "Namespace")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "Namespace")
		},
	})
}

func (m *Manager) registerDeploymentInformer() {
	informer := m.factory.Apps().V1().Deployments().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "Deployment")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "Deployment")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "Deployment")
		},
	})
}

func (m *Manager) registerStatefulSetInformer() {
	informer := m.factory.Apps().V1().StatefulSets().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "StatefulSet")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "StatefulSet")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "StatefulSet")
		},
	})
}

func (m *Manager) registerDaemonSetInformer() {
	informer := m.factory.Apps().V1().DaemonSets().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "DaemonSet")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "DaemonSet")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "DaemonSet")
		},
	})
}

func (m *Manager) registerReplicaSetInformer() {
	informer := m.factory.Apps().V1().ReplicaSets().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "ReplicaSet")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "ReplicaSet")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "ReplicaSet")
		},
	})
}

func (m *Manager) registerJobInformer() {
	informer := m.factory.Batch().V1().Jobs().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "Job")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "Job")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "Job")
		},
	})
}

func (m *Manager) registerCronJobInformer() {
	informer := m.factory.Batch().V1().CronJobs().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "CronJob")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "CronJob")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "CronJob")
		},
	})
}

func (m *Manager) registerIngressInformer() {
	informer := m.factory.Networking().V1().Ingresses().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "Ingress")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "Ingress")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "Ingress")
		},
	})
}

func (m *Manager) registerEndpointSliceInformer() {
	informer := m.factory.Discovery().V1().EndpointSlices().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "EndpointSlice")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "EndpointSlice")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "EndpointSlice")
		},
	})
}

func (m *Manager) registerStorageClassInformer() {
	informer := m.factory.Storage().V1().StorageClasses().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "StorageClass")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "StorageClass")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "StorageClass")
		},
	})
}

func (m *Manager) registerHPAInformer() {
	informer := m.factory.Autoscaling().V2().HorizontalPodAutoscalers().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "HorizontalPodAutoscaler")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "HorizontalPodAutoscaler")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "HorizontalPodAutoscaler")
		},
	})
}

func (m *Manager) registerPDBInformer() {
	informer := m.factory.Policy().V1().PodDisruptionBudgets().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onAdd(obj, "PodDisruptionBudget")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onUpdate(oldObj, newObj, "PodDisruptionBudget")
		},
		DeleteFunc: func(obj interface{}) {
			m.onDelete(obj, "PodDisruptionBudget")
		},
	})
}

// ListPodsBySelector lists pods matching a label selector
func (m *Manager) ListPodsBySelector(namespace string, selector labels.Selector) ([]*corev1.Pod, error) {
	if namespace != "" {
		return m.factory.Core().V1().Pods().Lister().Pods(namespace).List(selector)
	}
	return m.factory.Core().V1().Pods().Lister().List(selector)
}
