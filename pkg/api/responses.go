package api

import (
	"fmt"
	"time"

	"github.com/ammarlakis/astrolabe/pkg/graph"
	"k8s.io/apimachinery/pkg/types"
)

type Resource struct {
	Name               string                 `json:"name"`
	Namespace          string                 `json:"namespace"`
	Kind               string                 `json:"kind"`
	APIVersion         string                 `json:"apiVersion"`
	Status             string                 `json:"status"`
	Message            string                 `json:"message"`
	Chart              string                 `json:"chart"`
	Release            string                 `json:"release"`
	Age                string                 `json:"age"`
	CreationTimestamp  string                 `json:"creationTimestamp"`
	Image              string                 `json:"image,omitempty"`
	NodeName           string                 `json:"nodeName,omitempty"`
	RestartCount       int                    `json:"restartCount,omitempty"`
	Replicas           *graph.ReplicaInfo     `json:"replicas,omitempty"`
	OwnerReferences    []OwnerReference       `json:"ownerReferences,omitempty"`
	VolumeName         string                 `json:"volumeName,omitempty"`
	ClaimRef           *graph.ObjectReference `json:"claimRef,omitempty"`
	TargetPods         []string               `json:"targetPods,omitempty"`
	MountedPVCs        []string               `json:"mountedPVCs,omitempty"`
	UsedConfigMaps     []string               `json:"usedConfigMaps,omitempty"`
	UsedSecrets        []string               `json:"usedSecrets,omitempty"`
	ServiceAccountName string                 `json:"serviceAccountName,omitempty"`
}

type OwnerReference struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type GraphResponse struct {
	Nodes []NodeResponse `json:"nodes"`
	Edges []EdgeResponse `json:"edges"`
}

type NodeResponse struct {
	UID       string                  `json:"uid"`
	Name      string                  `json:"name"`
	Namespace string                  `json:"namespace"`
	Kind      string                  `json:"kind"`
	Status    string                  `json:"status"`
	Message   string                  `json:"message"`
	Chart     string                  `json:"chart,omitempty"`
	Release   string                  `json:"release,omitempty"`
	Metadata  *graph.ResourceMetadata `json:"metadata,omitempty"`
}

type EdgeResponse struct {
	Type string `json:"type"`
	From string `json:"from"`
	To   string `json:"to"`
}

// Resource represents a resource in the API response (compatible with datasource)
func (s *Server) nodesToResources(nodes []*graph.Node) []Resource {
	resources := make([]Resource, 0, len(nodes))

	// Build a cache of all UIDs we might need to lookup
	// This reduces lock contention by doing fewer GetNode calls
	uidCache := make(map[types.UID]*graph.Node)

	// Pre-populate cache with all UIDs we'll need
	for _, node := range nodes {
		// Cache UIDs from edges
		for _, edge := range node.IncomingEdges {
			if _, cached := uidCache[edge.FromUID]; !cached {
				if n, exists := s.graph.GetNode(edge.FromUID); exists {
					uidCache[edge.FromUID] = n
				}
			}
		}
		for _, edge := range node.OutgoingEdges {
			if _, cached := uidCache[edge.ToUID]; !cached {
				if n, exists := s.graph.GetNode(edge.ToUID); exists {
					uidCache[edge.ToUID] = n
				}
			}
		}
	}

	// Now build resources using the cache
	for _, node := range nodes {
		resource := Resource{
			Name:              node.Name,
			Namespace:         node.Namespace,
			Kind:              node.Kind,
			APIVersion:        node.APIVersion,
			Status:            string(node.Status),
			Message:           node.StatusMessage,
			Chart:             node.HelmChart,
			Release:           node.HelmRelease,
			Age:               formatAge(node.CreationTimestamp),
			CreationTimestamp: node.CreationTimestamp.Format(time.RFC3339),
		}

		// Add metadata
		if node.Metadata != nil {
			resource.Image = node.Metadata.Image
			resource.NodeName = node.Metadata.NodeName
			resource.RestartCount = node.Metadata.RestartCount
			resource.Replicas = node.Metadata.Replicas
			resource.VolumeName = node.Metadata.VolumeName
			resource.ClaimRef = node.Metadata.ClaimRef
		}

		// Extract owner references using cache
		for _, edge := range node.IncomingEdges {
			if edge.Type == graph.EdgeOwnership {
				if ownerNode, exists := uidCache[edge.FromUID]; exists {
					resource.OwnerReferences = append(resource.OwnerReferences, OwnerReference{
						Kind: ownerNode.Kind,
						Name: ownerNode.Name,
					})
				}
			}
		}

		// Extract related resources using cache
		resource.TargetPods = s.getRelatedNodeNames(node, graph.EdgeServiceSelector, uidCache)
		resource.MountedPVCs = s.getRelatedNodeNames(node, graph.EdgePodVolume, uidCache)
		resource.UsedConfigMaps = s.getRelatedNodeNames(node, graph.EdgeConfigMapRef, uidCache)
		resource.UsedSecrets = s.getRelatedNodeNames(node, graph.EdgeSecretRef, uidCache)

		// Extract ServiceAccount using cache
		for _, edge := range node.OutgoingEdges {
			if edge.Type == graph.EdgeServiceAccount {
				if saNode, exists := uidCache[edge.ToUID]; exists {
					resource.ServiceAccountName = saNode.Name
					break
				}
			}
		}

		resources = append(resources, resource)
	}

	return resources
}

func (s *Server) getRelatedNodeNames(node *graph.Node, edgeType graph.EdgeType, cache map[types.UID]*graph.Node) []string {
	names := make([]string, 0)
	for _, edge := range node.OutgoingEdges {
		if edge.Type == edgeType {
			if relatedNode, exists := cache[edge.ToUID]; exists {
				names = append(names, relatedNode.Name)
			}
		}
	}
	return names
}

// GraphResponse represents the graph API response
func (s *Server) buildGraphResponse(nodes []*graph.Node) GraphResponse {
	nodeMap := make(map[string]bool)
	for _, node := range nodes {
		nodeMap[string(node.UID)] = true
	}

	resp := GraphResponse{
		Nodes: make([]NodeResponse, 0, len(nodes)),
		Edges: make([]EdgeResponse, 0),
	}

	for _, node := range nodes {
		resp.Nodes = append(resp.Nodes, NodeResponse{
			UID:       string(node.UID),
			Name:      node.Name,
			Namespace: node.Namespace,
			Kind:      node.Kind,
			Status:    string(node.Status),
			Message:   node.StatusMessage,
			Chart:     node.HelmChart,
			Release:   node.HelmRelease,
			Metadata:  node.Metadata,
		})

		// Add edges where both nodes are in the result set
		for _, edge := range node.OutgoingEdges {
			if nodeMap[string(edge.ToUID)] {
				resp.Edges = append(resp.Edges, EdgeResponse{
					Type: string(edge.Type),
					From: string(edge.FromUID),
					To:   string(edge.ToUID),
				})
			}
		}
	}

	return resp
}

func formatAge(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	} else if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	} else if duration < 24*time.Hour {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	} else {
		return fmt.Sprintf("%dd", int(duration.Hours()/24))
	}
}
