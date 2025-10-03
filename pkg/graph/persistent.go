package graph

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

// PersistenceBackend defines the interface for graph persistence
type PersistenceBackend interface {
	SaveNode(node *Node) error
	DeleteNode(uid types.UID) error
	GetNode(uid types.UID) (*Node, error)
	GetAllNodes() ([]*Node, error)
	SaveEdge(edge *Edge) error
	DeleteEdge(fromUID, toUID types.UID) error
	GetAllEdges() ([]*Edge, error)
	LoadGraph() (*Graph, error)
	SaveGraph(g *Graph) error
	Close() error
}

// PersistentGraph wraps a Graph with persistence capabilities
type PersistentGraph struct {
	*Graph
	backend     PersistenceBackend
	enabled     bool
	asyncWrites bool
	writeChan   chan writeOp
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

type writeOp struct {
	opType string // "saveNode", "deleteNode", "saveEdge", "deleteEdge"
	node   *Node
	edge   *Edge
	uid    types.UID
	toUID  types.UID
}

// NewPersistentGraph creates a new graph with persistence
func NewPersistentGraph(backend PersistenceBackend, asyncWrites bool) *PersistentGraph {
	pg := &PersistentGraph{
		Graph:       NewGraph(),
		backend:     backend,
		enabled:     backend != nil,
		asyncWrites: asyncWrites,
		stopChan:    make(chan struct{}),
	}

	if pg.enabled && asyncWrites {
		pg.writeChan = make(chan writeOp, 1000) // Buffer for async writes
		pg.startAsyncWriter()
	}

	return pg
}

// LoadFromBackend loads the graph from the persistence backend
func (pg *PersistentGraph) LoadFromBackend() error {
	if !pg.enabled {
		klog.Info("Persistence disabled, starting with empty graph")
		return nil
	}

	klog.Info("Loading graph from persistence backend...")
	start := time.Now()

	// Load graph from backend
	g, err := pg.backend.LoadGraph()
	if err != nil {
		return err
	}

	// Replace in-memory graph
	pg.Graph = g

	klog.Infof("Graph loaded from backend in %v: %d nodes", time.Since(start), len(pg.nodes))
	return nil
}

// AddNode adds a node and persists it
func (pg *PersistentGraph) AddNode(node *Node) {
	// Add to in-memory graph
	pg.Graph.AddNode(node)

	// Persist
	if pg.enabled {
		if pg.asyncWrites {
			select {
			case pg.writeChan <- writeOp{opType: "saveNode", node: node}:
			default:
				klog.Warning("Write channel full, dropping async write")
			}
		} else {
			if err := pg.backend.SaveNode(node); err != nil {
				klog.Errorf("Failed to persist node %s: %v", node.UID, err)
			}
		}
	}
}

// RemoveNode removes a node and deletes it from persistence
func (pg *PersistentGraph) RemoveNode(uid types.UID) {
	// Remove from in-memory graph
	pg.Graph.RemoveNode(uid)

	// Delete from persistence
	if pg.enabled {
		if pg.asyncWrites {
			select {
			case pg.writeChan <- writeOp{opType: "deleteNode", uid: uid}:
			default:
				klog.Warning("Write channel full, dropping async delete")
			}
		} else {
			if err := pg.backend.DeleteNode(uid); err != nil {
				klog.Errorf("Failed to delete node %s from persistence: %v", uid, err)
			}
		}
	}
}

// AddEdge adds an edge and persists it
func (pg *PersistentGraph) AddEdge(edge *Edge) bool {
	// Add to in-memory graph
	success := pg.Graph.AddEdge(edge)

	if !success {
		return false
	}

	// Persist
	if pg.enabled {
		if pg.asyncWrites {
			select {
			case pg.writeChan <- writeOp{opType: "saveEdge", edge: edge}:
			default:
				klog.Warning("Write channel full, dropping async edge write")
			}
		} else {
			if err := pg.backend.SaveEdge(edge); err != nil {
				klog.Errorf("Failed to persist edge %s->%s: %v", edge.FromUID, edge.ToUID, err)
			}
		}
	}

	return true
}

// RemoveEdge removes an edge and deletes it from persistence
func (pg *PersistentGraph) RemoveEdge(fromUID, toUID types.UID) {
	// Remove from in-memory graph
	pg.Graph.RemoveEdge(fromUID, toUID)

	// Delete from persistence
	if pg.enabled {
		if pg.asyncWrites {
			select {
			case pg.writeChan <- writeOp{opType: "deleteEdge", uid: fromUID, toUID: toUID}:
			default:
				klog.Warning("Write channel full, dropping async edge delete")
			}
		} else {
			if err := pg.backend.DeleteEdge(fromUID, toUID); err != nil {
				klog.Errorf("Failed to delete edge from persistence: %v", err)
			}
		}
	}
}

// Snapshot creates a full snapshot of the graph to persistence
func (pg *PersistentGraph) Snapshot() error {
	if !pg.enabled {
		return nil
	}

	klog.Info("Creating graph snapshot...")
	start := time.Now()

	if err := pg.backend.SaveGraph(pg.Graph); err != nil {
		return err
	}

	klog.Infof("Snapshot completed in %v", time.Since(start))
	return nil
}

// Close closes the persistent graph and flushes pending writes
func (pg *PersistentGraph) Close() error {
	if !pg.enabled {
		return nil
	}

	if pg.asyncWrites {
		// Stop async writer
		close(pg.stopChan)
		pg.wg.Wait()

		// Flush remaining writes
		close(pg.writeChan)
		for op := range pg.writeChan {
			pg.executeWriteOp(op)
		}
	}

	// Close backend
	return pg.backend.Close()
}

// startAsyncWriter starts the async write worker
func (pg *PersistentGraph) startAsyncWriter() {
	pg.wg.Add(1)
	go func() {
		defer pg.wg.Done()

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		batchSize := 100
		batch := make([]writeOp, 0, batchSize)

		for {
			select {
			case op := <-pg.writeChan:
				batch = append(batch, op)

				// Execute batch when full
				if len(batch) >= batchSize {
					pg.executeBatch(batch)
					batch = batch[:0]
				}

			case <-ticker.C:
				// Periodic flush
				if len(batch) > 0 {
					pg.executeBatch(batch)
					batch = batch[:0]
				}

			case <-pg.stopChan:
				// Final flush
				if len(batch) > 0 {
					pg.executeBatch(batch)
				}
				return
			}
		}
	}()
}

// executeBatch executes a batch of write operations
func (pg *PersistentGraph) executeBatch(batch []writeOp) {
	start := time.Now()

	for _, op := range batch {
		pg.executeWriteOp(op)
	}

	klog.V(4).Infof("Executed batch of %d writes in %v", len(batch), time.Since(start))
}

// executeWriteOp executes a single write operation
func (pg *PersistentGraph) executeWriteOp(op writeOp) {
	var err error

	switch op.opType {
	case "saveNode":
		err = pg.backend.SaveNode(op.node)
	case "deleteNode":
		err = pg.backend.DeleteNode(op.uid)
	case "saveEdge":
		err = pg.backend.SaveEdge(op.edge)
	case "deleteEdge":
		err = pg.backend.DeleteEdge(op.uid, op.toUID)
	}

	if err != nil {
		klog.Errorf("Failed to execute %s: %v", op.opType, err)
	}
}

// GetBackend returns the persistence backend
func (pg *PersistentGraph) GetBackend() PersistenceBackend {
	return pg.backend
}

// IsEnabled returns whether persistence is enabled
func (pg *PersistentGraph) IsEnabled() bool {
	return pg.enabled
}
