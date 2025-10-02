# Astrolabe Architecture

## Overview

Astrolabe is designed as a lightweight, efficient Kubernetes state server that maintains an in-memory graph of cluster resources and their relationships. It replaces the need for the Grafana datasource plugin to directly query the Kubernetes API, providing better performance, lower cluster load, and richer relationship data.

## Core Components

### 1. Graph Database (`pkg/graph`)

The graph is the central data structure that stores all Kubernetes resources as nodes and their relationships as edges.

#### Node Structure
```go
type Node struct {
    UID               types.UID              // Kubernetes UID (unique)
    Name              string                 // Resource name
    Namespace         string                 // Namespace (empty for cluster-scoped)
    Kind              string                 // Resource kind
    APIVersion        string                 // API version
    ResourceVersion   string                 // For optimistic concurrency
    Labels            map[string]string      // Labels
    Annotations       map[string]string      // Annotations
    CreationTimestamp time.Time              // Creation time
    Status            ResourceStatus         // Computed status
    StatusMessage     string                 // Status message
    HelmChart         string                 // Helm chart name
    HelmRelease       string                 // Helm release name
    Metadata          *ResourceMetadata      // Resource-specific metadata
    OutgoingEdges     map[types.UID]*Edge    // Edges from this node
    IncomingEdges     map[types.UID]*Edge    // Edges to this node
}
```

#### Edge Types

Edges represent relationships between resources:

- **Ownership** (`owns`): Parent-child relationships via `ownerReferences`
  - Example: Deployment → ReplicaSet → Pod
  
- **Service Selection** (`selects`): Service to Pod via label selectors
  - Example: Service → Pod
  
- **Service Endpoints** (`endpoints`): Service to EndpointSlice
  - Example: Service → EndpointSlice
  
- **Ingress Routing** (`routes-to`): Ingress to Service backends
  - Example: Ingress → Service
  
- **Volume Mounting** (`mounts`): Pod to PVC
  - Example: Pod → PVC
  
- **Volume Binding** (`binds`): PVC to PV
  - Example: PVC → PV
  
- **ConfigMap/Secret References** (`uses-configmap`, `uses-secret`): Resource to ConfigMap/Secret
  - Example: Pod → ConfigMap, Deployment → Secret
  
- **ServiceAccount** (`uses-sa`): Pod/Workload to ServiceAccount
  - Example: Pod → ServiceAccount
  
- **HPA Scaling** (`scales`): HPA to scale target
  - Example: HPA → Deployment

#### Indexing Strategy

The graph maintains multiple indexes for efficient queries:

1. **Primary Index**: `map[UID]*Node` - O(1) lookup by UID
2. **Namespace/Kind Index**: `map[namespace]map[kind][]*Node` - Fast queries by type
3. **Helm Release Index**: `map[release][]*Node` - Fast Helm release queries
4. **Label Index**: `map[labelKey]map[labelValue][]*Node` - Fast label selector queries

### 2. Informer Manager (`pkg/informers`)

The informer manager sets up Kubernetes shared informers for all tracked resource types.

#### Shared Informers

Uses Kubernetes `client-go` shared informers for efficiency:
- Single watch connection per resource type
- Shared cache across all consumers
- Automatic reconnection on failures
- Resync every 10 minutes (configurable)

#### Label Filtering

Informers can be configured with label selectors to reduce memory:
```go
factory := informers.NewSharedInformerFactoryWithOptions(
    clientset,
    resyncPeriod,
    informers.WithTweakListOptions(func(options *metav1.ListOptions) {
        options.LabelSelector = "app.kubernetes.io/managed-by=Helm"
    }),
)
```

#### Event Handling

Each informer registers handlers for three event types:
- **Add**: New resource created
- **Update**: Existing resource modified
- **Delete**: Resource removed

Events are dispatched to the appropriate processor.

### 3. Resource Processors (`pkg/processors`)

Processors handle resource-specific logic for converting Kubernetes objects to graph nodes and creating edges.

#### Processor Responsibilities

1. **Node Creation**: Convert Kubernetes object to graph node
2. **Status Computation**: Determine resource status (Ready/Pending/Error/Unknown)
3. **Metadata Extraction**: Extract resource-specific metadata
4. **Edge Creation**: Create relationships to other resources

#### Status Computation Examples

**Pod Status**:
```go
switch pod.Status.Phase {
case corev1.PodRunning:
    // Check container readiness
    for _, cs := range pod.Status.ContainerStatuses {
        if !cs.Ready {
            return StatusPending, "Container not ready"
        }
    }
    return StatusReady, "Pod is running"
case corev1.PodPending:
    return StatusPending, "Pod is pending"
case corev1.PodFailed:
    return StatusError, "Pod failed"
}
```

**Deployment Status**:
```go
desired := deployment.Spec.Replicas
ready := deployment.Status.ReadyReplicas

if ready == desired {
    return StatusReady, "All replicas ready"
}
if ready == 0 {
    return StatusError, "No replicas ready"
}
return StatusPending, "Partially ready"
```

#### Edge Creation Logic

**Ownership Edges**:
```go
for _, owner := range pod.GetOwnerReferences() {
    if ownerNode, exists := graph.GetNode(owner.UID); exists {
        graph.AddEdge(&Edge{
            Type:    EdgeOwnership,
            FromUID: owner.UID,
            ToUID:   pod.UID,
        })
    }
}
```

**Service Selector Edges**:
```go
if len(service.Spec.Selector) > 0 {
    pods := findNodesByLabelSelector(service.Namespace, "Pod", service.Spec.Selector)
    for _, pod := range pods {
        graph.AddEdge(&Edge{
            Type:    EdgeServiceSelector,
            FromUID: service.UID,
            ToUID:   pod.UID,
        })
    }
}
```

### 4. HTTP API Server (`pkg/api`)

The API server exposes the graph data via RESTful HTTP endpoints.

#### Endpoint Design

All endpoints follow REST conventions:
- `GET /health` - Health check
- `GET /api/v1/resources` - Query resources
- `GET /api/v1/releases` - List Helm releases
- `GET /api/v1/charts` - List Helm charts
- `GET /api/v1/namespaces` - List namespaces
- `GET /api/v1/graph` - Get graph data

#### Response Format

Responses are compatible with the existing Grafana datasource plugin:
```json
{
  "name": "my-app-7d8f9c5b6-abc12",
  "namespace": "default",
  "kind": "Pod",
  "status": "Ready",
  "message": "Pod is running",
  "chart": "my-app-1.0.0",
  "release": "my-app",
  "ownerReferences": [
    {"kind": "ReplicaSet", "name": "my-app-7d8f9c5b6"}
  ],
  "replicas": null,
  "image": "nginx:1.21",
  "nodeName": "node-1",
  "restartCount": 0
}
```

## Data Flow

### Startup Sequence

```
1. Initialize Kubernetes client
   ↓
2. Create empty graph
   ↓
3. Create informer manager with label selector
   ↓
4. Register informers for all resource types
   ↓
5. Start informers (begin watching)
   ↓
6. Wait for cache sync (initial list)
   ↓
7. Start HTTP API server
   ↓
8. Ready to serve requests
```

### Resource Update Flow

```
Kubernetes API
   ↓ (watch event)
Informer
   ↓ (event handler)
Processor
   ↓ (create/update node)
Graph
   ↓ (derive edges)
Graph (updated)
   ↓ (query)
HTTP API
   ↓ (response)
Grafana
```

### Edge Derivation Flow

When a node is added/updated:

1. **Create ownership edges**: Check `ownerReferences`, find owner nodes in graph
2. **Create selector edges**: For Services/PDBs, find matching Pods by labels
3. **Create volume edges**: For Pods, find referenced PVCs; for PVCs, find bound PVs
4. **Create config edges**: For Pods/Workloads, find referenced ConfigMaps/Secrets
5. **Create service account edges**: For Pods/Workloads, find ServiceAccount
6. **Create special edges**: HPA targets, Ingress backends, EndpointSlice targets

## Concurrency Model

### Thread Safety

The graph uses read-write locks for thread safety:
```go
type Graph struct {
    mu    sync.RWMutex
    nodes map[types.UID]*Node
    // ... indexes
}

func (g *Graph) AddNode(node *Node) {
    g.mu.Lock()
    defer g.mu.Unlock()
    // ... update graph
}

func (g *Graph) GetNode(uid types.UID) (*Node, bool) {
    g.mu.RLock()
    defer g.mu.RUnlock()
    // ... read graph
}
```

### Goroutine Usage

1. **Informer goroutines**: One per resource type (managed by shared informer factory)
2. **Event processing**: Sequential per resource type (via informer work queue)
3. **HTTP handlers**: One per request (standard Go HTTP server)

## Memory Management

### Memory Footprint

Approximate memory per resource:
- **Node**: ~500 bytes (base) + labels/annotations
- **Edge**: ~100 bytes
- **Indexes**: ~50 bytes per index entry

Example for 1000 Pods:
- Nodes: 500 KB
- Edges (avg 5 per Pod): 500 KB
- Indexes: 250 KB
- **Total**: ~1.25 MB

### Memory Optimization

1. **Label filtering**: Only track resources matching selector
2. **Inactive resource filtering**: Skip old ReplicaSets with 0 replicas
3. **Efficient indexing**: Use pointers, not copies
4. **No deep copies**: Share label/annotation maps where possible

## Performance Characteristics

### Time Complexity

- **Add/Update Node**: O(1) for node, O(L) for indexes (L = number of labels)
- **Remove Node**: O(1) for node, O(E + L) for edges and indexes (E = number of edges)
- **Get Node by UID**: O(1)
- **Get Nodes by Namespace/Kind**: O(1) index lookup
- **Get Nodes by Helm Release**: O(1) index lookup
- **Get Nodes by Label Selector**: O(L) where L = number of matching nodes

### Space Complexity

- **Nodes**: O(N) where N = number of resources
- **Edges**: O(E) where E = number of relationships
- **Indexes**: O(N × L) where L = average labels per resource

## Scalability

### Cluster Size

Tested configurations:
- **Small** (< 100 resources): < 50 MB memory, < 10m CPU
- **Medium** (100-1000 resources): < 150 MB memory, < 20m CPU
- **Large** (1000-10000 resources): < 500 MB memory, < 50m CPU

### Bottlenecks

1. **Initial sync**: CPU-intensive during startup (parallel processing helps)
2. **Label selector queries**: Can be slow with many labels (indexed)
3. **Large result sets**: JSON serialization overhead (pagination recommended)

## Reliability

### Error Handling

1. **Informer failures**: Automatic reconnection by client-go
2. **Processor errors**: Logged but don't crash the server
3. **API errors**: Return appropriate HTTP status codes

### Resync Strategy

- Periodic resync every 10 minutes
- Ensures eventual consistency
- Catches missed events

## Security

### RBAC

Requires read-only access to cluster resources:
```yaml
rules:
  - apiGroups: ["", "apps", "batch", ...]
    resources: [pods, deployments, ...]
    verbs: ["get", "list", "watch"]
```

### API Security

- No authentication/authorization (relies on network policies)
- Runs in-cluster with ClusterIP service
- Can be exposed via Ingress with auth proxy

## Future Improvements

1. **Persistent storage**: Optional disk-backed cache for faster restarts
2. **Metrics**: Prometheus metrics for monitoring
3. **Tracing**: OpenTelemetry support
4. **GraphQL**: More flexible query API
5. **WebSockets**: Real-time updates to clients
6. **Multi-cluster**: Federated graph across clusters
7. **CRD support**: Generic handling of custom resources
