# Astrolabe Server

Astrolabe is a lightweight, in-cluster Kubernetes state server that watches cluster resources and maintains a live graph of relationships.

## Features

- **Persistent Storage**: Optional Redis-backed persistence with automatic snapshots
- **In-Memory Graph Database**: Fast, efficient storage of Kubernetes resources and their relationships
- **Real-Time Updates**: Uses Kubernetes informers with shared caches for minimal overhead
- **Relationship Tracking**: Automatically derives edges between resources:
  - Ownership chains (Deployment → ReplicaSet → Pod)
  - Service selectors (Service → Pod)
  - Volume bindings (Pod → PVC → PV)
  - ConfigMap/Secret references
  - Ingress backends
  - HPA scale targets
  - And more...
- **Helm-Aware**: Tracks Helm releases and charts automatically
- **Label Filtering**: Optionally filter resources by labels to reduce memory footprint
- **Smart Release Filtering**: Automatically includes cluster-scoped resources (like PersistentVolumes) when querying by release

## Architecture

```
┌────────────────────────────────────────────────────────────┐
│                      Kubernetes Cluster                    │
│  ┌──────────┐  ┌──────────┐   ┌──────────┐  ┌──────────┐   │
│  │   Pods   │  │ Services │   │   PVCs   │  │   ...    │   │
│  └────┬─────┘  └────┬─────┘   └────┬─────┘  └────┬─────┘   │
│       │             │              │             │         │
│       └─────────────┴──────────────┴─────────────┘         │
│                          │                                 │
│                    ┌─────▼─────┐                           │
│                    │ Informers │                           │
│                    │  (Watch)  │                           │
│                    └─────┬─────┘                           │
│                          │                                 │
│                    ┌─────▼─────┐                           │
│                    │ Astrolabe │                           │
│                    │  (Graph)  │                           │
│                    └─────┬─────┘                           │
│                          │                                 │
│                    ┌─────▼─────┐                           │
│                    │ HTTP API  │                           │
│                    └─────┬─────┘                           │
└──────────────────────────┼─────────────────────────────────┘
                           │
                    ┌──────▼──────┐
                    │   Grafana   │
                    │  Astrolabe  │
                    │     App     │
                    └─────────────┘
```

## Key Capabilities

### Smart Resource Filtering

Astrolabe intelligently handles cluster-scoped resources when filtering by Helm release:

- **PersistentVolumes**: Automatically included when bound to PVCs in a release, even though PVs don't have Helm labels
- **Release Isolation**: Resources from other releases are excluded, even when connected through shared cluster resources
- **Direct Connections Only**: Unmanaged resources are only included if directly connected to release resources, preventing cross-contamination

### Efficient Resource Tracking

- **Shared Informers**: Single set of watchers for all resources, minimizing cluster load
- **Event-Driven Updates**: Real-time updates via Kubernetes watch API, no polling
- **Optimized Indexing**: Multiple indexes for fast lookups by namespace, kind, release, and labels
- **Label Filtering**: Optional filtering to track only relevant resources

### Persistence & Reliability

- **Redis Backend**: Optional persistence with automatic snapshots
- **Fast Recovery**: Quick startup by loading cached state from Redis
- **Graceful Shutdown**: Final snapshot on shutdown ensures no data loss
- **Async Writes**: Non-blocking persistence for better performance

## Installation

### Prerequisites

- Kubernetes cluster (1.24+)
- kubectl configured
- Go 1.21+ (for building from source)
- Docker & Docker Compose (for local development)
- Redis 7+ (optional, for persistence - included in docker-compose setup)

### Quick Start with Docker Compose (Recommended for Development)

```bash
# Start Astrolabe with Redis persistence
docker-compose up -d

# View logs
docker-compose logs -f astrolabe

# Test API
curl http://localhost:8080/health
curl http://localhost:8080/api/v1/releases

# Stop
docker-compose down
```

This starts:
- Redis 7 with RDB + AOF persistence
- Astrolabe with Redis persistence enabled
- Automatic periodic snapshots (every 5 minutes by default)
- Graph state survives restarts!

### Deploy to Kubernetes

1. **Build and push Docker image** (optional, if using custom image):
   ```bash
   make docker-build
   docker tag astrolabe:latest your-registry/astrolabe:latest
   docker push your-registry/astrolabe:latest
   ```

2. **Deploy to cluster**:

   **Without Persistence (In-Memory Only):**
   ```bash
   kubectl apply -f deploy/deployment.yaml
   ```

   **With Persistence (Recommended for Production):**
   ```bash
   # Deploy Astrolabe
   kubectl apply -f deploy/deployment.yaml

   # Enable persistence (requires Redis deployment)
   kubectl set env deployment/astrolabe \
     ENABLE_PERSISTENCE=true \
     REDIS_ADDR=redis:6379 \
     -n astrolabe-system
   ```

   Note: You'll need to deploy Redis separately. See the `docker-compose.yaml` for a reference Redis configuration.

   This creates:
   - Namespace: `astrolabe-system`
   - ServiceAccount with ClusterRole for read-only access
   - Deployment with 1 replica
   - ClusterIP Service on port 8080

3. **Verify deployment**:
   ```bash
   kubectl -n astrolabe-system get pods
   kubectl -n astrolabe-system logs -l app=astrolabe
   ```

### Local Development

1. **Clone the repository**:
   ```bash
   git clone <repository-url>
   cd astrolabe-server
   ```

2. **Install dependencies**:
   ```bash
   make deps
   ```

3. **Build**:
   ```bash
   make build
   ```

4. **Run locally** (requires kubeconfig):
   ```bash
   # In-memory only (no persistence)
   make run

   # Without filters (all resources)
   make run-all

   # With Redis persistence (requires Redis running)
   make run-persistent

   # Custom configuration
   ./bin/astrolabe --in-cluster=false --port=8080 --label-selector="" --v=2
   ```

## Configuration

### Command-Line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--kubeconfig` | `~/.kube/config` | Path to kubeconfig file |
| `--in-cluster` | `true` | Use in-cluster configuration |
| `--port` | `8080` | HTTP API server port |
| `--label-selector` | `""` | Label selector to filter resources (empty = all resources) |
| `--enable-persistence` | `false` | Enable Redis persistence |
| `--redis-addr` | `localhost:6379` | Redis server address |
| `--redis-password` | `""` | Redis password |
| `--redis-db` | `0` | Redis database number |
| `--snapshot-interval` | `300` | Snapshot interval in seconds (0 = disabled) |
| `--v` | `0` | Log verbosity level (0-4) |

### Environment Variables

- `KUBECONFIG`: Path to kubeconfig file (overridden by `--kubeconfig` flag)
- `LABEL_SELECTOR`: Label selector to filter resources (overridden by `--label-selector` flag)
- `ENABLE_PERSISTENCE`: Enable Redis persistence (`true`/`false`)
- `REDIS_ADDR`: Redis server address
- `REDIS_PASSWORD`: Redis password
- `REDIS_DB`: Redis database number

### Label Filtering

By default, Astrolabe tracks all resources in the cluster. You can optionally filter resources by labels to reduce memory usage in large clusters.

To filter only Helm-managed resources:
```bash
--label-selector="app.kubernetes.io/managed-by=Helm"
```

To use custom filters:
```bash
--label-selector="environment=production,team=platform"
```

**Note**: PersistentVolumes are always tracked regardless of label selector, as they are cluster-scoped and typically don't have Helm labels but are needed for complete resource graphs.

## API Reference

### Health Check

```
GET /health
```

Response:
```json
{
  "status": "healthy",
  "nodes": 150
}
```

### Get Resources

```
GET /api/v1/resources?release=<release-name>&namespace=<namespace>
```

Query Parameters:
- `release` (optional): Filter by Helm release name
- `namespace` (optional): Filter by namespace

Response: Array of resources with metadata

**Smart Filtering**: When filtering by `release`, the API automatically includes cluster-scoped resources (like `PersistentVolume`) that are bound to resources in the release. This ensures complete resource graphs even when cluster-scoped resources don't have Helm labels.

### Get Releases

```
GET /api/v1/releases?namespace=<namespace>
```

Query Parameters:
- `namespace` (optional): Filter by namespace

Response: Array of Helm release names

### Get Charts

```
GET /api/v1/charts?namespace=<namespace>
```

Query Parameters:
- `namespace` (optional): Filter by namespace

Response: Array of Helm chart names

### Get Namespaces

```
GET /api/v1/namespaces
```

Response: Array of namespace names

### Get Graph

```
GET /api/v1/graph?release=<release-name>&namespace=<namespace>
```

Query Parameters:
- `release` (optional): Filter by Helm release name
- `namespace` (optional): Filter by namespace

Response:
```json
{
  "nodes": [
    {
      "uid": "abc-123",
      "name": "my-app",
      "namespace": "default",
      "kind": "Deployment",
      "status": "Ready",
      "message": "All replicas ready (3/3)",
      "chart": "my-app-1.0.0",
      "release": "my-app",
      "metadata": {
        "replicas": {
          "desired": 3,
          "current": 3,
          "ready": 3,
          "available": 3
        }
      }
    }
  ],
  "edges": [
    {
      "type": "owns",
      "from": "abc-123",
      "to": "def-456"
    }
  ]
}
```

## Persistence

Astrolabe supports optional Redis-backed persistence to survive restarts and maintain state across deployments.

### How It Works

1. **Automatic Snapshots**: Astrolabe periodically saves the entire graph to Redis (default: every 5 minutes)
2. **On-Demand Snapshots**: Manual snapshots are created on graceful shutdown
3. **Startup Recovery**: On startup, Astrolabe loads the last snapshot from Redis and continues watching for updates
4. **Async Writes**: Individual resource updates are written asynchronously for better performance
5. **Graceful Degradation**: If Redis is unavailable, Astrolabe continues operating in memory-only mode

### Configuration

Enable persistence via environment variables or command-line flags:

```bash
# Environment variables (recommended for Docker/Kubernetes)
ENABLE_PERSISTENCE=true
REDIS_ADDR=redis:6379
REDIS_PASSWORD=your-password  # optional
REDIS_DB=0

# Command-line flags
./astrolabe --enable-persistence=true --redis-addr=redis:6379 --snapshot-interval=300
```

### Redis Configuration

For production use, configure Redis with both RDB and AOF persistence:

```conf
# RDB snapshots
save 900 1
save 300 10
save 60 10000

# AOF persistence
appendonly yes
appendfsync everysec
```

See `redis.conf` in the repository for a complete example.

## Integration with Grafana

To use Astrolabe with the Grafana Astrolabe App:

1. **Expose Astrolabe service** (if Grafana is outside the cluster):
   ```bash
   kubectl -n astrolabe-system port-forward svc/astrolabe 8080:8080
   ```

   Or create an Ingress/LoadBalancer service.

2. **Configure the Astrolabe App** in Grafana:
   - URL: `http://astrolabe.astrolabe-system.svc.cluster.local:8080` (in-cluster)
   - Or: `http://localhost:8080` (port-forward)

3. The app will use Astrolabe's API endpoints to visualize your Kubernetes resources and their relationships.

## Resource Types Tracked

### Core Resources
- Pods
- Services
- ServiceAccounts
- ConfigMaps
- Secrets
- PersistentVolumeClaims
- PersistentVolumes
- Namespaces

### Workloads
- Deployments
- StatefulSets
- DaemonSets
- ReplicaSets
- Jobs
- CronJobs

### Networking
- Ingresses
- EndpointSlices

### Storage
- StorageClasses

### Autoscaling
- HorizontalPodAutoscalers

### Policy
- PodDisruptionBudgets

## Edge Types

| Edge Type | Description | Example |
|-----------|-------------|---------|
| `owns` | Ownership relationship | Deployment → ReplicaSet → Pod |
| `selects` | Service selector | Service → Pod |
| `endpoints` | Service endpoints | Service → EndpointSlice |
| `routes-to` | Ingress backend | Ingress → Service |
| `mounts` | Volume mount | Pod → PVC |
| `binds` | Volume binding | PVC → PV |
| `uses-configmap` | ConfigMap reference | Pod → ConfigMap |
| `uses-secret` | Secret reference | Pod → Secret |
| `uses-sa` | ServiceAccount | Pod → ServiceAccount |
| `scales` | HPA target | HPA → Deployment |

## Performance

### Memory Usage

Typical memory usage varies based on cluster size and label filtering:
- Small cluster (50-100 resources): ~50-100 MB
- Medium cluster (500-1000 resources): ~150-300 MB
- Large cluster (5000+ resources): ~500 MB - 1 GB

**Tip**: Use label selectors to reduce memory footprint in large clusters.

### CPU Usage

- Startup: ~50-100m CPU (during initial sync and cache warm-up)
- Steady state: ~10-20m CPU
- Updates: Minimal overhead (event-driven with informers)
- Persistence: Additional ~5-10m CPU for periodic snapshots (when enabled)

### Storage (with Redis Persistence)

- Graph snapshot size: ~1-5 MB per 1000 resources
- Redis memory: Similar to Astrolabe memory usage
- Snapshot frequency: Configurable (default: 5 minutes)

### Network

- Uses Kubernetes watch API (efficient long-polling)
- Minimal bandwidth usage after initial sync
- No polling overhead
- Redis: Minimal traffic (only on updates and snapshots)

## Troubleshooting

### Check logs

```bash
kubectl -n astrolabe-system logs -l app=astrolabe -f
```

### Increase verbosity

Edit deployment and set `--v=4` for debug logging.

### Check RBAC permissions

```bash
kubectl auth can-i list pods --as=system:serviceaccount:astrolabe-system:astrolabe
```

### Test API

```bash
kubectl -n astrolabe-system port-forward svc/astrolabe 8080:8080
curl http://localhost:8080/health
curl http://localhost:8080/api/v1/releases
```

## Development

### Project Structure

```
.
├── cmd/
│   └── astrolabe/          # Main application
│       └── main.go
├── pkg/
│   ├── api/                # HTTP API server
│   │   ├── server.go       # API handlers and routing
│   │   ├── helpers.go      # Helper functions for filtering
│   │   └── types.go        # API response types
│   ├── graph/              # Graph data structures
│   │   ├── types.go        # Core graph implementation
│   │   └── persistent.go   # Redis-backed persistent graph
│   ├── informers/          # Kubernetes informers
│   │   ├── manager.go      # Informer lifecycle management
│   │   └── handlers.go     # Event handlers
│   ├── processors/         # Resource processors
│   │   ├── base.go         # Base processor interface
│   │   ├── core.go         # Core resources (Pods, Services, etc.)
│   │   ├── workloads.go    # Workload resources (Deployments, etc.)
│   │   ├── networking.go   # Network resources (Ingress, etc.)
│   │   └── registry.go     # Processor registry
│   └── storage/            # Persistence layer
│       └── redis.go        # Redis backend implementation
├── deploy/                 # Kubernetes manifests
│   └── deployment.yaml
├── docker-compose.yaml     # Local development with Redis
├── redis.conf              # Redis configuration
├── Dockerfile
├── Makefile
├── go.mod
└── README.md
```

### Adding New Resource Types

1. **Add informer** in `pkg/informers/manager.go`:
   - Register the informer with appropriate event handlers
   - Consider if label filtering should apply

2. **Create processor** in `pkg/processors/`:
   - Implement the `Processor` interface
   - Extract relevant metadata and status
   - Define relationship edges to other resources

3. **Register processor** in `pkg/processors/registry.go`:
   - Add to the processor registry

4. **Update RBAC** in `deploy/deployment.yaml`:
   - Add necessary permissions to the ClusterRole

### Running Tests

```bash
make test
```

### Code Formatting

```bash
make fmt
```

### Building and Running

```bash
# Build binary
make build

# Run locally (development)
make run

# Build and run with Docker Compose
make up
make logs
```

## Future Enhancements

- [ ] Metrics export (Prometheus)
- [ ] GraphQL API
- [ ] Multi-cluster support
- [ ] Advanced filtering and search
- [ ] WebSocket support for real-time updates
- [ ] Helm release metadata extraction (values, hooks, etc.)
- [ ] Custom resource definitions (CRDs) support
- [ ] PostgreSQL backend option (alternative to Redis)
- [ ] Resource history tracking and time-series queries
- [ ] Performance benchmarks and optimization

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

Astrolabe is open source under the [GNU Affero General Public License v3.0 (AGPLv3)](LICENSE).
Contributions are welcome. Commercial use is allowed, provided that modifications and derived works are also open-sourced under the same license.
