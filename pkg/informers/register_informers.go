package informers

import (
	"fmt"

	"github.com/ammarlakis/astrolabe/pkg/processors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

func (m *Manager) register(kind string, informer cache.SharedIndexInformer) error {

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.onEvent(obj, kind, processors.EventAdd)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.onEvent(newObj, kind, processors.EventUpdate)
		},
		DeleteFunc: func(obj interface{}) {
			m.onEvent(obj, kind, processors.EventDelete)
		},
	}
	_, err := informer.AddEventHandler(handler)
	if err != nil {
		klog.Errorf("Failed to register %s informer: %v", kind, err)
		return err
	}
	klog.V(2).Infof("Registered %s informer", kind)
	return nil
}

// registerInformers registers all resource informers
func (m *Manager) registerInformers() error {
	type pair struct {
		kind     string
		informer cache.SharedIndexInformer
	}
	registers := []pair{
		{
			kind:     "Pod",
			informer: m.factory.Core().V1().Pods().Informer(),
		},
		{
			kind:     "Service",
			informer: m.factory.Core().V1().Services().Informer(),
		},
		{
			kind:     "ServiceAccount",
			informer: m.factory.Core().V1().ServiceAccounts().Informer(),
		},
		{
			kind:     "ConfigMap",
			informer: m.factory.Core().V1().ConfigMaps().Informer(),
		},
		{
			kind:     "Secret",
			informer: m.factory.Core().V1().Secrets().Informer(),
		},
		{
			kind:     "PersistentVolumeClaim",
			informer: m.factory.Core().V1().PersistentVolumeClaims().Informer(),
		},
		{
			kind:     "Namespace",
			informer: m.factory.Core().V1().Namespaces().Informer(),
		},
		{
			kind:     "PersistentVolume",
			informer: m.factory.Core().V1().PersistentVolumes().Informer(),
		},
		{
			kind:     "StorageClass",
			informer: m.factory.Storage().V1().StorageClasses().Informer(),
		},
		{
			kind:     "HorizontalPodAutoscaler",
			informer: m.factory.Autoscaling().V1().HorizontalPodAutoscalers().Informer(),
		},
		{
			kind:     "PodDisruptionBudget",
			informer: m.factory.Policy().V1().PodDisruptionBudgets().Informer(),
		},
		{
			kind:     "Deployment",
			informer: m.factory.Apps().V1().Deployments().Informer(),
		},
		{
			kind:     "StatefulSet",
			informer: m.factory.Apps().V1().StatefulSets().Informer(),
		},
		{
			kind:     "DaemonSet",
			informer: m.factory.Apps().V1().DaemonSets().Informer(),
		},
		{
			kind:     "ReplicaSet",
			informer: m.factory.Apps().V1().ReplicaSets().Informer(),
		},
		{
			kind:     "Job",
			informer: m.factory.Batch().V1().Jobs().Informer(),
		},
		{
			kind:     "CronJob",
			informer: m.factory.Batch().V1().CronJobs().Informer(),
		},
		{
			kind:     "Ingress",
			informer: m.factory.Networking().V1().Ingresses().Informer(),
		},
		{
			kind:     "EndpointSlice",
			informer: m.factory.Discovery().V1().EndpointSlices().Informer(),
		},
	}

	var errors []error

	for _, register := range registers {
		if err := m.register(register.kind, register.informer); err != nil {
			klog.Errorf("Failed to register %s informer: %v", register.kind, err)
			errors = append(errors, err)
		}
		klog.V(2).Infof("Registered %s informer", register.kind)
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to register informers: %v", errors)
	}
	return nil
}
