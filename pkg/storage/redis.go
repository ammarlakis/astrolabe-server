package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ammarlakis/astrolabe/pkg/graph"
	"github.com/redis/go-redis/v9"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

const (
	// Redis key prefixes
	nodeKeyPrefix     = "astrolabe:node:"
	edgeKeyPrefix     = "astrolabe:edge:"
	indexKeyPrefix    = "astrolabe:index:"
	metadataKey       = "astrolabe:metadata"
	
	// Index keys
	namespaceKindIndex = "astrolabe:index:ns-kind:"
	helmReleaseIndex   = "astrolabe:index:helm-release:"
	labelIndex         = "astrolabe:index:label:"
)

// RedisStore provides persistent storage for the graph using Redis
type RedisStore struct {
	client *redis.Client
	ctx    context.Context
}

// NewRedisStore creates a new Redis store
func NewRedisStore(addr, password string, db int) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 5,
	})
	
	ctx := context.Background()
	
	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}
	
	klog.Info("Successfully connected to Redis")
	
	return &RedisStore{
		client: client,
		ctx:    ctx,
	}, nil
}

// Close closes the Redis connection
func (s *RedisStore) Close() error {
	return s.client.Close()
}

// SaveNode persists a node to Redis
func (s *RedisStore) SaveNode(node *graph.Node) error {
	// Serialize node (without edges to avoid circular references)
	nodeData := &SerializedNode{
		UID:               node.UID,
		Name:              node.Name,
		Namespace:         node.Namespace,
		Kind:              node.Kind,
		APIVersion:        node.APIVersion,
		ResourceVersion:   node.ResourceVersion,
		Labels:            node.Labels,
		Annotations:       node.Annotations,
		CreationTimestamp: node.CreationTimestamp,
		Status:            node.Status,
		StatusMessage:     node.StatusMessage,
		HelmChart:         node.HelmChart,
		HelmRelease:       node.HelmRelease,
		Metadata:          node.Metadata,
	}
	
	data, err := json.Marshal(nodeData)
	if err != nil {
		return fmt.Errorf("failed to marshal node: %w", err)
	}
	
	// Save node
	key := nodeKeyPrefix + string(node.UID)
	if err := s.client.Set(s.ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("failed to save node to Redis: %w", err)
	}
	
	// Update indexes
	if err := s.updateIndexes(node); err != nil {
		klog.Errorf("Failed to update indexes for node %s: %v", node.UID, err)
	}
	
	return nil
}

// DeleteNode removes a node from Redis
func (s *RedisStore) DeleteNode(uid types.UID) error {
	// Get node first to update indexes
	node, err := s.GetNode(uid)
	if err != nil {
		klog.V(4).Infof("Node %s not found in Redis, skipping delete", uid)
		return nil
	}
	
	// Delete node
	key := nodeKeyPrefix + string(uid)
	if err := s.client.Del(s.ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete node from Redis: %w", err)
	}
	
	// Remove from indexes
	if err := s.removeFromIndexes(node); err != nil {
		klog.Errorf("Failed to remove node from indexes: %v", err)
	}
	
	// Delete associated edges
	if err := s.deleteNodeEdges(uid); err != nil {
		klog.Errorf("Failed to delete edges for node %s: %v", uid, err)
	}
	
	return nil
}

// GetNode retrieves a node from Redis
func (s *RedisStore) GetNode(uid types.UID) (*graph.Node, error) {
	key := nodeKeyPrefix + string(uid)
	data, err := s.client.Get(s.ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("node not found: %s", uid)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get node from Redis: %w", err)
	}
	
	var nodeData SerializedNode
	if err := json.Unmarshal(data, &nodeData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node: %w", err)
	}
	
	// Convert to graph.Node
	node := &graph.Node{
		UID:               nodeData.UID,
		Name:              nodeData.Name,
		Namespace:         nodeData.Namespace,
		Kind:              nodeData.Kind,
		APIVersion:        nodeData.APIVersion,
		ResourceVersion:   nodeData.ResourceVersion,
		Labels:            nodeData.Labels,
		Annotations:       nodeData.Annotations,
		CreationTimestamp: nodeData.CreationTimestamp,
		Status:            nodeData.Status,
		StatusMessage:     nodeData.StatusMessage,
		HelmChart:         nodeData.HelmChart,
		HelmRelease:       nodeData.HelmRelease,
		Metadata:          nodeData.Metadata,
		OutgoingEdges:     make(map[types.UID]*graph.Edge),
		IncomingEdges:     make(map[types.UID]*graph.Edge),
	}
	
	return node, nil
}

// GetAllNodes retrieves all nodes from Redis
func (s *RedisStore) GetAllNodes() ([]*graph.Node, error) {
	// Scan for all node keys
	var cursor uint64
	var nodes []*graph.Node
	
	for {
		keys, nextCursor, err := s.client.Scan(s.ctx, cursor, nodeKeyPrefix+"*", 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan nodes: %w", err)
		}
		
		for _, key := range keys {
			uid := types.UID(key[len(nodeKeyPrefix):])
			node, err := s.GetNode(uid)
			if err != nil {
				klog.Errorf("Failed to get node %s: %v", uid, err)
				continue
			}
			nodes = append(nodes, node)
		}
		
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	
	return nodes, nil
}

// SaveEdge persists an edge to Redis
func (s *RedisStore) SaveEdge(edge *graph.Edge) error {
	data, err := json.Marshal(edge)
	if err != nil {
		return fmt.Errorf("failed to marshal edge: %w", err)
	}
	
	// Save edge with composite key: from:to
	key := edgeKeyPrefix + string(edge.FromUID) + ":" + string(edge.ToUID)
	if err := s.client.Set(s.ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("failed to save edge to Redis: %w", err)
	}
	
	return nil
}

// DeleteEdge removes an edge from Redis
func (s *RedisStore) DeleteEdge(fromUID, toUID types.UID) error {
	key := edgeKeyPrefix + string(fromUID) + ":" + string(toUID)
	if err := s.client.Del(s.ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete edge from Redis: %w", err)
	}
	return nil
}

// GetAllEdges retrieves all edges from Redis
func (s *RedisStore) GetAllEdges() ([]*graph.Edge, error) {
	var cursor uint64
	var edges []*graph.Edge
	
	for {
		keys, nextCursor, err := s.client.Scan(s.ctx, cursor, edgeKeyPrefix+"*", 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan edges: %w", err)
		}
		
		for _, key := range keys {
			data, err := s.client.Get(s.ctx, key).Bytes()
			if err != nil {
				klog.Errorf("Failed to get edge %s: %v", key, err)
				continue
			}
			
			var edge graph.Edge
			if err := json.Unmarshal(data, &edge); err != nil {
				klog.Errorf("Failed to unmarshal edge: %v", err)
				continue
			}
			
			edges = append(edges, &edge)
		}
		
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	
	return edges, nil
}

// LoadGraph loads the entire graph from Redis
func (s *RedisStore) LoadGraph() (*graph.Graph, error) {
	klog.Info("Loading graph from Redis...")
	start := time.Now()
	
	g := graph.NewGraph()
	
	// Load all nodes
	nodes, err := s.GetAllNodes()
	if err != nil {
		return nil, fmt.Errorf("failed to load nodes: %w", err)
	}
	
	klog.Infof("Loaded %d nodes from Redis", len(nodes))
	
	// Add nodes to graph
	for _, node := range nodes {
		g.AddNode(node)
	}
	
	// Load all edges
	edges, err := s.GetAllEdges()
	if err != nil {
		return nil, fmt.Errorf("failed to load edges: %w", err)
	}
	
	klog.Infof("Loaded %d edges from Redis", len(edges))
	
	// Add edges to graph
	for _, edge := range edges {
		g.AddEdge(edge)
	}
	
	klog.Infof("Graph loaded from Redis in %v", time.Since(start))
	
	return g, nil
}

// SaveGraph saves the entire graph to Redis
func (s *RedisStore) SaveGraph(g *graph.Graph) error {
	klog.Info("Saving graph to Redis...")
	start := time.Now()
	
	nodes := g.GetAllNodes()
	
	// Save all nodes
	for _, node := range nodes {
		if err := s.SaveNode(node); err != nil {
			klog.Errorf("Failed to save node %s: %v", node.UID, err)
		}
	}
	
	// Save all edges
	edgeCount := 0
	for _, node := range nodes {
		for _, edge := range node.OutgoingEdges {
			if err := s.SaveEdge(edge); err != nil {
				klog.Errorf("Failed to save edge: %v", err)
			} else {
				edgeCount++
			}
		}
	}
	
	klog.Infof("Saved %d nodes and %d edges to Redis in %v", len(nodes), edgeCount, time.Since(start))
	
	return nil
}

// Helper functions

func (s *RedisStore) updateIndexes(node *graph.Node) error {
	// Namespace/Kind index
	nsKey := node.Namespace
	if nsKey == "" {
		nsKey = "_cluster"
	}
	indexKey := namespaceKindIndex + nsKey + ":" + node.Kind
	if err := s.client.SAdd(s.ctx, indexKey, string(node.UID)).Err(); err != nil {
		return err
	}
	
	// Helm release index
	if node.HelmRelease != "" {
		indexKey := helmReleaseIndex + node.HelmRelease
		if err := s.client.SAdd(s.ctx, indexKey, string(node.UID)).Err(); err != nil {
			return err
		}
	}
	
	// Label indexes
	for key, value := range node.Labels {
		indexKey := labelIndex + key + ":" + value
		if err := s.client.SAdd(s.ctx, indexKey, string(node.UID)).Err(); err != nil {
			return err
		}
	}
	
	return nil
}

func (s *RedisStore) removeFromIndexes(node *graph.Node) error {
	// Namespace/Kind index
	nsKey := node.Namespace
	if nsKey == "" {
		nsKey = "_cluster"
	}
	indexKey := namespaceKindIndex + nsKey + ":" + node.Kind
	s.client.SRem(s.ctx, indexKey, string(node.UID))
	
	// Helm release index
	if node.HelmRelease != "" {
		indexKey := helmReleaseIndex + node.HelmRelease
		s.client.SRem(s.ctx, indexKey, string(node.UID))
	}
	
	// Label indexes
	for key, value := range node.Labels {
		indexKey := labelIndex + key + ":" + value
		s.client.SRem(s.ctx, indexKey, string(node.UID))
	}
	
	return nil
}

func (s *RedisStore) deleteNodeEdges(uid types.UID) error {
	// Delete all edges where this node is from or to
	pattern := edgeKeyPrefix + string(uid) + ":*"
	if err := s.deleteKeysByPattern(pattern); err != nil {
		return err
	}
	
	pattern = edgeKeyPrefix + "*:" + string(uid)
	if err := s.deleteKeysByPattern(pattern); err != nil {
		return err
	}
	
	return nil
}

func (s *RedisStore) deleteKeysByPattern(pattern string) error {
	var cursor uint64
	for {
		keys, nextCursor, err := s.client.Scan(s.ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		
		if len(keys) > 0 {
			if err := s.client.Del(s.ctx, keys...).Err(); err != nil {
				return err
			}
		}
		
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}

// SerializedNode is a node without edges for serialization
type SerializedNode struct {
	UID               types.UID              `json:"uid"`
	Name              string                 `json:"name"`
	Namespace         string                 `json:"namespace"`
	Kind              string                 `json:"kind"`
	APIVersion        string                 `json:"apiVersion"`
	ResourceVersion   string                 `json:"resourceVersion"`
	Labels            map[string]string      `json:"labels"`
	Annotations       map[string]string      `json:"annotations"`
	CreationTimestamp time.Time              `json:"creationTimestamp"`
	Status            graph.ResourceStatus   `json:"status"`
	StatusMessage     string                 `json:"statusMessage"`
	HelmChart         string                 `json:"helmChart,omitempty"`
	HelmRelease       string                 `json:"helmRelease,omitempty"`
	Metadata          *graph.ResourceMetadata `json:"metadata,omitempty"`
}

// GetStats returns Redis statistics
func (s *RedisStore) GetStats() (map[string]interface{}, error) {
	info, err := s.client.Info(s.ctx, "stats", "memory").Result()
	if err != nil {
		return nil, err
	}
	
	dbSize, err := s.client.DBSize(s.ctx).Result()
	if err != nil {
		return nil, err
	}
	
	return map[string]interface{}{
		"db_size": dbSize,
		"info":    info,
	}, nil
}
