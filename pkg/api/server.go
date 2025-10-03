package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ammarlakis/astrolabe/pkg/graph"
	"k8s.io/klog/v2"
)

// Server is the HTTP API server
type Server struct {
	graph  graph.GraphInterface
	port   int
	server *http.Server
}

// NewServer creates a new API server
func NewServer(g graph.GraphInterface, port int) *Server {
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

		nodes = s.includePersistentVolumes(nodes, releaseName)
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

		nodes = s.includePersistentVolumes(nodes, "")
	}

	// Convert to response format compatible with the datasource
	resources := s.nodesToResources(nodes)

	if releaseName != "" {
		for i := range resources {
			if resources[i].Release == "" {
				resources[i].Release = releaseName
			}
		}
	}

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
		nodes = s.expandRelatedNodes(nodes, namespace, releaseName)
		nodes = s.includePersistentVolumes(nodes, releaseName)
	} else if namespace != "" {
		allNodes := s.graph.GetAllNodes()
		for _, node := range allNodes {
			if node.Namespace == namespace || node.Namespace == "" {
				nodes = append(nodes, node)
			}
		}
		nodes = s.includePersistentVolumes(nodes, "")
	} else {
		nodes = s.graph.GetAllNodes()
		nodes = s.includePersistentVolumes(nodes, "")
	}

	// Build graph response with nodes and edges
	graphResp := s.buildGraphResponse(nodes)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(graphResp)
}
