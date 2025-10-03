package processors

import (
	"fmt"

	"github.com/ammarlakis/astrolabe/pkg/graph"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/klog/v2"
)

// DeploymentProcessor processes Deployment resources
type DeploymentProcessor struct {
	*BaseProcessor
}

func NewDeploymentProcessor(g graph.GraphInterface) *DeploymentProcessor {
	return &DeploymentProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *DeploymentProcessor) Process(obj interface{}, eventType EventType) error {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		return fmt.Errorf("expected Deployment, got %T", obj)
	}

	if eventType == EventDelete {
		return p.handleDelete(deployment, "Deployment")
	}

	// Create or update node
	node := graph.NewNodeFromObject(deployment, "Deployment", "apps/v1")

	// Set status
	node.Status, node.StatusMessage = p.getDeploymentStatus(deployment)

	// Set metadata
	node.Metadata = &graph.ResourceMetadata{
		Replicas: &graph.ReplicaInfo{
			Desired:   getInt32Value(deployment.Spec.Replicas, 1),
			Current:   deployment.Status.Replicas,
			Ready:     deployment.Status.ReadyReplicas,
			Available: deployment.Status.AvailableReplicas,
		},
	}

	// Extract image from first container
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		node.Metadata.Image = deployment.Spec.Template.Spec.Containers[0].Image
	}

	// Add node to graph
	p.graph.AddNode(node)

	// Create ownership edges
	p.createOwnershipEdges(node, deployment.GetOwnerReferences())

	// Create edges to ConfigMaps and Secrets
	p.createConfigMapSecretEdges(node, &deployment.Spec.Template.Spec)

	// Create edge to ServiceAccount
	if deployment.Spec.Template.Spec.ServiceAccountName != "" {
		if saNode := p.findNodeByNamespaceKindName(deployment.Namespace, "ServiceAccount", deployment.Spec.Template.Spec.ServiceAccountName); saNode != nil {
			p.createEdgeIfNodeExists(node.UID, saNode.UID, graph.EdgeServiceAccount)
		}
	}

	return nil
}

func (p *DeploymentProcessor) getDeploymentStatus(deployment *appsv1.Deployment) (graph.ResourceStatus, string) {
	desired := getInt32Value(deployment.Spec.Replicas, 1)
	ready := deployment.Status.ReadyReplicas

	if desired == 0 && ready == 0 {
		return graph.StatusReady, "Scaled to zero (0/0)"
	}

	if ready == desired {
		return graph.StatusReady, fmt.Sprintf("All replicas ready (%d/%d)", ready, desired)
	}

	if ready == 0 && desired > 0 {
		return graph.StatusError, fmt.Sprintf("No replicas ready (0/%d)", desired)
	}

	return graph.StatusPending, fmt.Sprintf("Partially ready (%d/%d)", ready, desired)
}

// StatefulSetProcessor processes StatefulSet resources
type StatefulSetProcessor struct {
	*BaseProcessor
}

func NewStatefulSetProcessor(g graph.GraphInterface) *StatefulSetProcessor {
	return &StatefulSetProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *StatefulSetProcessor) Process(obj interface{}, eventType EventType) error {
	sts, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		return fmt.Errorf("expected StatefulSet, got %T", obj)
	}

	if eventType == EventDelete {
		return p.handleDelete(sts, "StatefulSet")
	}

	node := graph.NewNodeFromObject(sts, "StatefulSet", "apps/v1")
	node.Status, node.StatusMessage = p.getStatefulSetStatus(sts)

	node.Metadata = &graph.ResourceMetadata{
		Replicas: &graph.ReplicaInfo{
			Desired:   getInt32Value(sts.Spec.Replicas, 1),
			Current:   sts.Status.Replicas,
			Ready:     sts.Status.ReadyReplicas,
			Available: sts.Status.AvailableReplicas,
		},
	}

	if len(sts.Spec.Template.Spec.Containers) > 0 {
		node.Metadata.Image = sts.Spec.Template.Spec.Containers[0].Image
	}

	p.graph.AddNode(node)
	p.createOwnershipEdges(node, sts.GetOwnerReferences())
	p.createConfigMapSecretEdges(node, &sts.Spec.Template.Spec)

	if sts.Spec.Template.Spec.ServiceAccountName != "" {
		if saNode := p.findNodeByNamespaceKindName(sts.Namespace, "ServiceAccount", sts.Spec.Template.Spec.ServiceAccountName); saNode != nil {
			p.createEdgeIfNodeExists(node.UID, saNode.UID, graph.EdgeServiceAccount)
		}
	}

	return nil
}

func (p *StatefulSetProcessor) getStatefulSetStatus(sts *appsv1.StatefulSet) (graph.ResourceStatus, string) {
	desired := getInt32Value(sts.Spec.Replicas, 1)
	ready := sts.Status.ReadyReplicas

	if desired == 0 && ready == 0 {
		return graph.StatusReady, "Scaled to zero (0/0)"
	}

	if ready == desired {
		return graph.StatusReady, fmt.Sprintf("All replicas ready (%d/%d)", ready, desired)
	}

	if ready == 0 && desired > 0 {
		return graph.StatusError, fmt.Sprintf("No replicas ready (0/%d)", desired)
	}

	return graph.StatusPending, fmt.Sprintf("Partially ready (%d/%d)", ready, desired)
}

// DaemonSetProcessor processes DaemonSet resources
type DaemonSetProcessor struct {
	*BaseProcessor
}

func NewDaemonSetProcessor(g graph.GraphInterface) *DaemonSetProcessor {
	return &DaemonSetProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *DaemonSetProcessor) Process(obj interface{}, eventType EventType) error {
	ds, ok := obj.(*appsv1.DaemonSet)
	if !ok {
		return fmt.Errorf("expected DaemonSet, got %T", obj)
	}

	if eventType == EventDelete {
		return p.handleDelete(ds, "DaemonSet")
	}

	node := graph.NewNodeFromObject(ds, "DaemonSet", "apps/v1")
	node.Status, node.StatusMessage = p.getDaemonSetStatus(ds)

	node.Metadata = &graph.ResourceMetadata{
		Replicas: &graph.ReplicaInfo{
			Desired:   ds.Status.DesiredNumberScheduled,
			Current:   ds.Status.CurrentNumberScheduled,
			Ready:     ds.Status.NumberReady,
			Available: ds.Status.NumberAvailable,
		},
	}

	if len(ds.Spec.Template.Spec.Containers) > 0 {
		node.Metadata.Image = ds.Spec.Template.Spec.Containers[0].Image
	}

	p.graph.AddNode(node)
	p.createOwnershipEdges(node, ds.GetOwnerReferences())
	p.createConfigMapSecretEdges(node, &ds.Spec.Template.Spec)

	if ds.Spec.Template.Spec.ServiceAccountName != "" {
		if saNode := p.findNodeByNamespaceKindName(ds.Namespace, "ServiceAccount", ds.Spec.Template.Spec.ServiceAccountName); saNode != nil {
			p.createEdgeIfNodeExists(node.UID, saNode.UID, graph.EdgeServiceAccount)
		}
	}

	return nil
}

func (p *DaemonSetProcessor) getDaemonSetStatus(ds *appsv1.DaemonSet) (graph.ResourceStatus, string) {
	desired := ds.Status.DesiredNumberScheduled
	ready := ds.Status.NumberReady

	if desired == 0 && ready == 0 {
		return graph.StatusReady, "No nodes to schedule (0/0)"
	}

	if ready == desired {
		return graph.StatusReady, fmt.Sprintf("All pods ready (%d/%d)", ready, desired)
	}

	if ready == 0 && desired > 0 {
		return graph.StatusError, fmt.Sprintf("No pods ready (0/%d)", desired)
	}

	return graph.StatusPending, fmt.Sprintf("Partially ready (%d/%d)", ready, desired)
}

// ReplicaSetProcessor processes ReplicaSet resources
type ReplicaSetProcessor struct {
	*BaseProcessor
}

func NewReplicaSetProcessor(g graph.GraphInterface) *ReplicaSetProcessor {
	return &ReplicaSetProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *ReplicaSetProcessor) Process(obj interface{}, eventType EventType) error {
	rs, ok := obj.(*appsv1.ReplicaSet)
	if !ok {
		return fmt.Errorf("expected ReplicaSet, got %T", obj)
	}

	if eventType == EventDelete {
		return p.handleDelete(rs, "ReplicaSet")
	}

	// Skip inactive ReplicaSets (old versions with 0 replicas)
	if rs.Status.Replicas == 0 && rs.Status.ReadyReplicas == 0 {
		klog.V(4).Infof("Skipping inactive ReplicaSet: %s/%s", rs.Namespace, rs.Name)
		return nil
	}

	node := graph.NewNodeFromObject(rs, "ReplicaSet", "apps/v1")
	node.Status, node.StatusMessage = p.getReplicaSetStatus(rs)

	node.Metadata = &graph.ResourceMetadata{
		Replicas: &graph.ReplicaInfo{
			Desired:   getInt32Value(rs.Spec.Replicas, 1),
			Current:   rs.Status.Replicas,
			Ready:     rs.Status.ReadyReplicas,
			Available: rs.Status.AvailableReplicas,
		},
	}

	if len(rs.Spec.Template.Spec.Containers) > 0 {
		node.Metadata.Image = rs.Spec.Template.Spec.Containers[0].Image
	}

	p.graph.AddNode(node)
	p.createOwnershipEdges(node, rs.GetOwnerReferences())
	p.createConfigMapSecretEdges(node, &rs.Spec.Template.Spec)

	if rs.Spec.Template.Spec.ServiceAccountName != "" {
		if saNode := p.findNodeByNamespaceKindName(rs.Namespace, "ServiceAccount", rs.Spec.Template.Spec.ServiceAccountName); saNode != nil {
			p.createEdgeIfNodeExists(node.UID, saNode.UID, graph.EdgeServiceAccount)
		}
	}

	return nil
}

func (p *ReplicaSetProcessor) getReplicaSetStatus(rs *appsv1.ReplicaSet) (graph.ResourceStatus, string) {
	desired := getInt32Value(rs.Spec.Replicas, 1)
	ready := rs.Status.ReadyReplicas

	if desired == 0 && ready == 0 {
		return graph.StatusReady, "Scaled to zero (0/0)"
	}

	if ready == desired {
		return graph.StatusReady, fmt.Sprintf("All replicas ready (%d/%d)", ready, desired)
	}

	if ready == 0 && desired > 0 {
		return graph.StatusError, fmt.Sprintf("No replicas ready (0/%d)", desired)
	}

	return graph.StatusPending, fmt.Sprintf("Partially ready (%d/%d)", ready, desired)
}

// JobProcessor processes Job resources
type JobProcessor struct {
	*BaseProcessor
}

func NewJobProcessor(g graph.GraphInterface) *JobProcessor {
	return &JobProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *JobProcessor) Process(obj interface{}, eventType EventType) error {
	job, ok := obj.(*batchv1.Job)
	if !ok {
		return fmt.Errorf("expected Job, got %T", obj)
	}

	if eventType == EventDelete {
		return p.handleDelete(job, "Job")
	}

	node := graph.NewNodeFromObject(job, "Job", "batch/v1")
	node.Status, node.StatusMessage = p.getJobStatus(job)

	if len(job.Spec.Template.Spec.Containers) > 0 {
		node.Metadata = &graph.ResourceMetadata{
			Image: job.Spec.Template.Spec.Containers[0].Image,
		}
	}

	p.graph.AddNode(node)
	p.createOwnershipEdges(node, job.GetOwnerReferences())
	p.createConfigMapSecretEdges(node, &job.Spec.Template.Spec)

	if job.Spec.Template.Spec.ServiceAccountName != "" {
		if saNode := p.findNodeByNamespaceKindName(job.Namespace, "ServiceAccount", job.Spec.Template.Spec.ServiceAccountName); saNode != nil {
			p.createEdgeIfNodeExists(node.UID, saNode.UID, graph.EdgeServiceAccount)
		}
	}

	return nil
}

func (p *JobProcessor) getJobStatus(job *batchv1.Job) (graph.ResourceStatus, string) {
	if job.Status.Succeeded > 0 {
		return graph.StatusReady, "Job completed successfully"
	}

	if job.Status.Failed > 0 {
		return graph.StatusError, fmt.Sprintf("Job failed (%d failures)", job.Status.Failed)
	}

	if job.Status.Active > 0 {
		return graph.StatusPending, "Job is running"
	}

	return graph.StatusPending, "Job is pending"
}

// CronJobProcessor processes CronJob resources
type CronJobProcessor struct {
	*BaseProcessor
}

func NewCronJobProcessor(g graph.GraphInterface) *CronJobProcessor {
	return &CronJobProcessor{BaseProcessor: NewBaseProcessor(g)}
}

func (p *CronJobProcessor) Process(obj interface{}, eventType EventType) error {
	cronJob, ok := obj.(*batchv1.CronJob)
	if !ok {
		return fmt.Errorf("expected CronJob, got %T", obj)
	}

	if eventType == EventDelete {
		return p.handleDelete(cronJob, "CronJob")
	}

	node := graph.NewNodeFromObject(cronJob, "CronJob", "batch/v1")

	activeCount := len(cronJob.Status.Active)
	if activeCount > 0 {
		node.Status = graph.StatusPending
		node.StatusMessage = fmt.Sprintf("%d active job(s)", activeCount)
	} else {
		node.Status = graph.StatusReady
		node.StatusMessage = "CronJob scheduled"
	}

	if len(cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers) > 0 {
		node.Metadata = &graph.ResourceMetadata{
			Image: cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image,
		}
	}

	p.graph.AddNode(node)
	p.createOwnershipEdges(node, cronJob.GetOwnerReferences())
	p.createConfigMapSecretEdges(node, &cronJob.Spec.JobTemplate.Spec.Template.Spec)

	if cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName != "" {
		if saNode := p.findNodeByNamespaceKindName(cronJob.Namespace, "ServiceAccount", cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName); saNode != nil {
			p.createEdgeIfNodeExists(node.UID, saNode.UID, graph.EdgeServiceAccount)
		}
	}

	return nil
}

// Helper functions

func getInt32Value(ptr *int32, defaultValue int32) int32 {
	if ptr == nil {
		return defaultValue
	}
	return *ptr
}
