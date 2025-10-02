package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ammarlakis/astrolabe/pkg/graph"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

// GraphInterface defines the interface for graph operations
type GraphInterface interface {
	GetNode(uid types.UID) (*graph.Node, bool)
	GetAllNodes() []*graph.Node
	GetNodesByHelmRelease(release string) []*graph.Node
	GetAllHelmReleases() []string
	GetAllHelmCharts() []string
}

// Server is the HTTP API server
type Server struct {
	graph  GraphInterface
	port   int
	server *http.Server
}

// NewServer creates a new API server
func NewServer(g GraphInterface, port int) *Server {
	return &Server{
		graph: g,
		port:  port,
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()
	
	// Register handlers
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/resources", s.handleResources)
	mux.HandleFunc("/api/v1/releases", s.handleReleases)
	mux.HandleFunc("/api/v1/charts", s.handleCharts)
	mux.HandleFunc("/api/v1/namespaces", s.handleNamespaces)
	mux.HandleFunc("/api/v1/graph", s.handleGraph)
	
	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      s.loggingMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	
	klog.Infof("Starting API server on port %d", s.port)
	return s.server.ListenAndServe()
}

// Stop stops the HTTP server
func (s *Server) Stop() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

// Middleware

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		klog.V(2).Infof("API: %s %s (took %v)", r.Method, r.RequestURI, time.Since(start))
	})
}

// Handlers

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"nodes":  len(s.graph.GetAllNodes()),
	})
}

func (s *Server) handleResources(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	query := r.URL.Query()
	releaseName := query.Get("release")
	namespace := query.Get("namespace")
	
	klog.V(2).Infof("API: /resources request - release=%s namespace=%s", releaseName, namespace)
	
	var nodes []*graph.Node
	
	if releaseName != "" {
		// Get resources by Helm release
		nodes = s.graph.GetNodesByHelmRelease(releaseName)
		klog.V(2).Infof("API: Found %d nodes for release '%s' (from cache)", len(nodes), releaseName)
		
		// Filter by namespace if specified
		if namespace != "" {
			filtered := make([]*graph.Node, 0)
			for _, node := range nodes {
				if node.Namespace == namespace || node.Namespace == "" {
					filtered = append(filtered, node)
				}
			}
			nodes = filtered
		}
	} else {
		// Get all nodes
		nodes = s.graph.GetAllNodes()
		
		// Filter by namespace if specified
		if namespace != "" {
			filtered := make([]*graph.Node, 0)
			for _, node := range nodes {
				if node.Namespace == namespace || node.Namespace == "" {
					filtered = append(filtered, node)
				}
			}
			nodes = filtered
		}
	}
	
	// Convert to response format compatible with the datasource
	resources := s.nodesToResources(nodes)
	
	klog.V(2).Infof("API: Returning %d resources (took %v)", len(resources), time.Since(start))
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resources)
}

func (s *Server) handleReleases(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	namespace := query.Get("namespace")
	
	releases := s.graph.GetAllHelmReleases()
	
	// Filter by namespace if specified
	if namespace != "" {
		filtered := make([]string, 0)
		for _, release := range releases {
			// Check if release has resources in the namespace
			nodes := s.graph.GetNodesByHelmRelease(release)
			for _, node := range nodes {
				if node.Namespace == namespace {
					filtered = append(filtered, release)
					break
				}
			}
		}
		releases = filtered
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(releases)
}

func (s *Server) handleCharts(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	namespace := query.Get("namespace")
	
	charts := s.graph.GetAllHelmCharts()
	
	// Filter by namespace if specified
	if namespace != "" {
		filtered := make([]string, 0)
		chartSet := make(map[string]bool)
		
		nodes := s.graph.GetAllNodes()
		for _, node := range nodes {
			if node.Namespace == namespace && node.HelmChart != "" {
				if !chartSet[node.HelmChart] {
					filtered = append(filtered, node.HelmChart)
					chartSet[node.HelmChart] = true
				}
			}
		}
		charts = filtered
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(charts)
}

func (s *Server) handleNamespaces(w http.ResponseWriter, r *http.Request) {
	namespaces := make(map[string]bool)
	
	nodes := s.graph.GetAllNodes()
	for _, node := range nodes {
		if node.Namespace != "" {
			namespaces[node.Namespace] = true
		}
	}
	
	result := make([]string, 0, len(namespaces))
	for ns := range namespaces {
		result = append(result, ns)
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	releaseName := query.Get("release")
	namespace := query.Get("namespace")
	
	var nodes []*graph.Node
	
	if releaseName != "" {
		nodes = s.graph.GetNodesByHelmRelease(releaseName)
		if namespace != "" {
			filtered := make([]*graph.Node, 0)
			for _, node := range nodes {
				if node.Namespace == namespace || node.Namespace == "" {
					filtered = append(filtered, node)
				}
			}
			nodes = filtered
		}
	} else if namespace != "" {
		allNodes := s.graph.GetAllNodes()
		for _, node := range allNodes {
			if node.Namespace == namespace || node.Namespace == "" {
				nodes = append(nodes, node)
			}
		}
	} else {
		nodes = s.graph.GetAllNodes()
	}
	
	// Build graph response with nodes and edges
	graphResp := s.buildGraphResponse(nodes)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(graphResp)
}

// Helper functions

// Resource represents a resource in the API response (compatible with datasource)
type Resource struct {
	Name              string                 `json:"name"`
	Namespace         string                 `json:"namespace"`
	Kind              string                 `json:"kind"`
	APIVersion        string                 `json:"apiVersion"`
	Status            string                 `json:"status"`
	Message           string                 `json:"message"`
	Chart             string                 `json:"chart"`
	Release           string                 `json:"release"`
	Age               string                 `json:"age"`
	CreationTimestamp string                 `json:"creationTimestamp"`
	Image             string                 `json:"image,omitempty"`
	NodeName          string                 `json:"nodeName,omitempty"`
	RestartCount      int                    `json:"restartCount,omitempty"`
	Replicas          *graph.ReplicaInfo     `json:"replicas,omitempty"`
	OwnerReferences   []OwnerReference       `json:"ownerReferences,omitempty"`
	VolumeName        string                 `json:"volumeName,omitempty"`
	ClaimRef          *graph.ObjectReference `json:"claimRef,omitempty"`
	TargetPods        []string               `json:"targetPods,omitempty"`
	MountedPVCs       []string               `json:"mountedPVCs,omitempty"`
	UsedConfigMaps    []string               `json:"usedConfigMaps,omitempty"`
	UsedSecrets       []string               `json:"usedSecrets,omitempty"`
	ServiceAccountName string                `json:"serviceAccountName,omitempty"`
}

type OwnerReference struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

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
		resource.TargetPods = s.getRelatedNodeNamesFromCache(node, graph.EdgeServiceSelector, uidCache)
		resource.MountedPVCs = s.getRelatedNodeNamesFromCache(node, graph.EdgePodVolume, uidCache)
		resource.UsedConfigMaps = s.getRelatedNodeNamesFromCache(node, graph.EdgeConfigMapRef, uidCache)
		resource.UsedSecrets = s.getRelatedNodeNamesFromCache(node, graph.EdgeSecretRef, uidCache)
		
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

func (s *Server) getRelatedNodeNames(node *graph.Node, edgeType graph.EdgeType) []string {
	names := make([]string, 0)
	for _, edge := range node.OutgoingEdges {
		if edge.Type == edgeType {
			if relatedNode, exists := s.graph.GetNode(edge.ToUID); exists {
				names = append(names, relatedNode.Name)
			}
		}
	}
	return names
}

func (s *Server) getRelatedNodeNamesFromCache(node *graph.Node, edgeType graph.EdgeType, cache map[types.UID]*graph.Node) []string {
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
type GraphResponse struct {
	Nodes []NodeResponse `json:"nodes"`
	Edges []EdgeResponse `json:"edges"`
}

type NodeResponse struct {
	UID       string                 `json:"uid"`
	Name      string                 `json:"name"`
	Namespace string                 `json:"namespace"`
	Kind      string                 `json:"kind"`
	Status    string                 `json:"status"`
	Message   string                 `json:"message"`
	Chart     string                 `json:"chart,omitempty"`
	Release   string                 `json:"release,omitempty"`
	Metadata  *graph.ResourceMetadata `json:"metadata,omitempty"`
}

type EdgeResponse struct {
	Type string `json:"type"`
	From string `json:"from"`
	To   string `json:"to"`
}

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
