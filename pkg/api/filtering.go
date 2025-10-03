package api

import (
	"strings"

	"github.com/ammarlakis/astrolabe/pkg/graph"
	"k8s.io/apimachinery/pkg/types"
)

// expandRelatedNodes performs a breadth-first traversal to include related resources.
// releaseName is used to filter out resources from other Helm releases during traversal.
func (s *Server) expandRelatedNodes(base []*graph.Node, namespace string, releaseName string) []*graph.Node {
	if len(base) == 0 {
		return base
	}

	allowedKinds := map[string]struct{}{
		"pod":                   {},
		"replicaset":            {},
		"endpointslice":         {},
		"configmap":             {},
		"secret":                {},
		"serviceaccount":        {},
		"service":               {},
		"persistentvolume":      {},
		"persistentvolumeclaim": {},
		"storageclass":          {},
	}

	withinNamespace := func(node *graph.Node) bool {
		if namespace == "" {
			return true
		}
		if node.Namespace == "" {
			return true
		}
		return node.Namespace == namespace
	}

	// Track nodes that belong to the requested release
	releaseNodes := make(map[types.UID]bool)
	for _, node := range base {
		if releaseName != "" && node.HelmRelease == releaseName {
			releaseNodes[node.UID] = true
		}
	}

	seen := make(map[types.UID]*graph.Node, len(base))
	queue := make([]*graph.Node, 0, len(base))
	ordered := make([]*graph.Node, 0, len(base))

	for _, node := range base {
		if _, exists := seen[node.UID]; exists {
			continue
		}
		seen[node.UID] = node
		queue = append(queue, node)
		ordered = append(ordered, node)
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		neighbours := make([]*graph.Node, 0, len(current.OutgoingEdges)+len(current.IncomingEdges))

		for _, edge := range current.OutgoingEdges {
			if neighbour, exists := s.graph.GetNode(edge.ToUID); exists {
				neighbours = append(neighbours, neighbour)
			}
		}
		for _, edge := range current.IncomingEdges {
			if neighbour, exists := s.graph.GetNode(edge.FromUID); exists {
				neighbours = append(neighbours, neighbour)
			}
		}

		for _, neighbour := range neighbours {
			if _, exists := seen[neighbour.UID]; exists {
				continue
			}

			if !withinNamespace(neighbour) {
				continue
			}

			// For unmanaged resources (no release tag), only include if directly connected to a release resource
			if releaseName != "" && neighbour.HelmRelease == "" {
				if !releaseNodes[current.UID] {
					continue
				}
			}

			// Skip resources from other Helm releases
			if releaseName != "" && neighbour.HelmRelease != "" && neighbour.HelmRelease != releaseName {
				continue
			}

			kind := strings.ToLower(neighbour.Kind)
			if _, allowed := allowedKinds[kind]; !allowed {
				continue
			}

			seen[neighbour.UID] = neighbour
			queue = append(queue, neighbour)
			ordered = append(ordered, neighbour)
		}
	}

	return ordered
}

// includePersistentVolumes adds PVs bound to PVCs that belong to the specified release.
// If releaseName is empty, it includes PVs for all PVCs in the node set.
func (s *Server) includePersistentVolumes(nodes []*graph.Node, releaseName string) []*graph.Node {
	if len(nodes) == 0 {
		return nodes
	}
	seen := make(map[types.UID]struct{}, len(nodes))
	for _, node := range nodes {
		seen[node.UID] = struct{}{}
	}

	initialLen := len(nodes)
	var pvByName map[string]*graph.Node

	addPV := func(pvNode *graph.Node) {
		if pvNode == nil {
			return
		}
		if _, alreadyIncluded := seen[pvNode.UID]; alreadyIncluded {
			return
		}
		nodes = append(nodes, pvNode)
		seen[pvNode.UID] = struct{}{}
	}

	for i := 0; i < initialLen; i++ {
		node := nodes[i]
		if strings.ToLower(node.Kind) != "persistentvolumeclaim" {
			continue
		}

		// Skip PVCs that don't belong to the requested release
		if releaseName != "" && node.HelmRelease != releaseName {
			continue
		}

		// Try to find PV via edge first
		for _, edge := range node.OutgoingEdges {
			if edge.Type != graph.EdgePVCBinding {
				continue
			}

			if pvNode, exists := s.graph.GetNode(edge.ToUID); exists {
				addPV(pvNode)
			}
		}

		// Fallback: lookup by volumeName if no edge exists
		if node.Metadata == nil || node.Metadata.VolumeName == "" {
			continue
		}

		if pvByName == nil {
			pvByName = make(map[string]*graph.Node)
			for _, candidate := range s.graph.GetAllNodes() {
				if strings.ToLower(candidate.Kind) == "persistentvolume" {
					pvByName[candidate.Name] = candidate
				}
			}
		}

		if pvNode, exists := pvByName[node.Metadata.VolumeName]; exists {
			addPV(pvNode)
		}
	}

	return nodes
}
