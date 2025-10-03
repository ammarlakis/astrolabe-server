package informers

import (
	"context"
	"fmt"
	"time"

	"github.com/ammarlakis/astrolabe/pkg/graph"
	"github.com/ammarlakis/astrolabe/pkg/processors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	defaultResyncPeriod = 10 * time.Minute
)

// Manager manages all Kubernetes informers and updates the graph
type Manager struct {
	clientset     *kubernetes.Clientset
	graph         graph.GraphInterface
	factory       informers.SharedInformerFactory
	stopCh        chan struct{}
	labelSelector string

	// Processors for different resource types
	processors *processors.ProcessorRegistry
}

// NewManager creates a new informer manager
func NewManager(clientset *kubernetes.Clientset, g graph.GraphInterface, labelSelector string) *Manager {
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

func (m *Manager) onEvent(obj interface{}, kind string, eventType processors.EventType) {
	klog.V(2).Infof("Cache: %s %s", string(eventType), kind)
	m.processors.Process(obj, kind, eventType)
}
