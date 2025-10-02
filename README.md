# Astrolabe - Kubernetes State Server

Astrolabe is a lightweight, in-cluster Kubernetes state server that watches cluster resources and maintains a live graph of relationships. It's designed to replace direct cluster access for the Grafana Kubernetes datasource plugin, providing efficient querying and real-time updates.

## Features

- **🔥 NEW: Persistent Storage**: Redis-backed persistence - survives pod restarts! ([See PERSISTENCE.md](PERSISTENCE.md))
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
- **HTTP API**: RESTful API compatible with the existing Grafana datasource plugin
- **Resource Status**: Intelligent status detection for all resource types

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Kubernetes Cluster                      │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │   Pods   │  │ Services │  │   PVCs   │  │   ...    │   │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘   │
│       │             │              │             │          │
│       └─────────────┴──────────────┴─────────────┘          │
│                          │                                   │
│                    ┌─────▼─────┐                            │
│                    │  Informers │                            │
│                    │  (Watch)   │                            │
│                    └─────┬─────┘                            │
│                          │                                   │
│                    ┌─────▼─────┐                            │
│                    │ Astrolabe │                            │
│                    │  (Graph)  │                            │
│                    └─────┬─────┘                            │
│                          │                                   │
│                    ┌─────▼─────┐                            │
│                    │ HTTP API  │                            │
│                    └─────┬─────┘                            │
└──────────────────────────┼─────────────────────────────────┘
                           │
                    ┌──────▼──────┐
                    │   Grafana   │
                    │ Datasource  │
                    └─────────────┘
```

## Installation

### Prerequisites

- Kubernetes cluster (1.24+)
- kubectl configured
- Go 1.21+ (for building from source)
- Docker & Docker Compose (for local development with persistence)
- Redis 7+ (optional, for persistence)

### Quick Start with Docker Compose (Recommended for Development)

```bash
# Start Astrolabe with Redis persistence
cd kubernetes-state-server
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
- Redis with RDB + AOF persistence
- Astrolabe with persistence enabled
- Graph survives restarts!

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
   # Deploy Redis first
   kubectl apply -f deploy/redis.yaml
   
   # Deploy Astrolabe with persistence enabled
   kubectl apply -f deploy/deployment.yaml
   kubectl set env deployment/astrolabe ENABLE_PERSISTENCE=true REDIS_ADDR=redis:6379 -n astrolabe-system
   ```

   This creates:
   - Namespace: `astrolabe-system`
   - ServiceAccount with ClusterRole for read-only access
   - Deployment with 1 replica
   - ClusterIP Service
   - (Optional) Redis with persistent volume

3. **Verify deployment**:
   ```bash
   kubectl -n astrolabe-system get pods
   kubectl -n astrolabe-system logs -l app=astrolabe
   ```

### Local Development

1. **Clone the repository**:
   ```bash
   git clone <repository-url>
   cd kubernetes-state-server
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
   # With Helm filter (default)
   make run
   
   # Without filters (all resources)
   make run-all
   
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
| `--label-selector` | `app.kubernetes.io/managed-by=Helm` | Label selector to filter resources |
| `--enable-persistence` | `false` | Enable Redis persistence |
| `--redis-addr` | `localhost:6379` | Redis server address |
| `--redis-password` | `""` | Redis password |
| `--redis-db` | `0` | Redis database number |
| `--snapshot-interval` | `300` | Snapshot interval in seconds |
| `--v` | `0` | Log verbosity level (0-4) |

### Environment Variables

- `KUBECONFIG`: Path to kubeconfig file (overridden by `--kubeconfig` flag)
- `ENABLE_PERSISTENCE`: Enable Redis persistence (`true`/`false`)
- `REDIS_ADDR`: Redis server address
- `REDIS_PASSWORD`: Redis password
- `REDIS_DB`: Redis database number

### Label Filtering

By default, Astrolabe only tracks resources managed by Helm (`app.kubernetes.io/managed-by=Helm`). This significantly reduces memory usage in large clusters.

To track all resources:
```bash
--label-selector=""
```

To use custom filters:
```bash
--label-selector="environment=production,team=platform"
```

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

Response: Array of resources compatible with Grafana datasource format

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

## Integration with Grafana Datasource

To use Astrolabe with the Grafana Kubernetes datasource plugin:

1. **Expose Astrolabe service** (if Grafana is outside the cluster):
   ```bash
   kubectl -n astrolabe-system port-forward svc/astrolabe 8080:8080
   ```
   
   Or create an Ingress/LoadBalancer service.

2. **Configure datasource** in Grafana:
   - URL: `http://astrolabe.astrolabe-system.svc.cluster.local:8080` (in-cluster)
   - Or: `http://localhost:8080` (port-forward)

3. **Update datasource plugin** to use Astrolabe endpoints instead of direct Kubernetes API.

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

Typical memory usage (with Helm filter):
- Small cluster (50 Helm resources): ~50 MB
- Medium cluster (500 Helm resources): ~150 MB
- Large cluster (5000 Helm resources): ~500 MB

### CPU Usage

- Startup: ~100m CPU (during initial sync)
- Steady state: ~10-20m CPU
- Updates: Minimal overhead (event-driven)

### Network

- Uses Kubernetes watch API (efficient long-polling)
- Minimal bandwidth usage after initial sync
- No polling overhead

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
│   │   └── server.go
│   ├── graph/              # Graph data structures
│   │   └── types.go
│   ├── informers/          # Kubernetes informers
│   │   └── manager.go
│   └── processors/         # Resource processors
│       ├── base.go
│       ├── core.go
│       ├── workloads.go
│       ├── networking.go
│       └── registry.go
├── deploy/                 # Kubernetes manifests
│   └── deployment.yaml
├── Dockerfile
├── Makefile
├── go.mod
└── README.md
```

### Adding New Resource Types

1. Add informer registration in `pkg/informers/manager.go`
2. Create processor in `pkg/processors/`
3. Register processor in `pkg/processors/registry.go`
4. Update RBAC in `deploy/deployment.yaml`

### Running Tests

```bash
make test
```

### Code Formatting

```bash
make fmt
```

## Comparison with Direct Kubernetes Access

| Feature | Astrolabe | Direct K8s API |
|---------|-----------|----------------|
| Query Speed | Fast (in-memory) | Slower (API calls) |
| Relationship Queries | Native support | Manual joins |
| Resource Overhead | Low (shared informers) | High (multiple watchers) |
| Network Usage | Minimal | Higher |
| Cluster Load | Low | Higher |
| Setup Complexity | Medium | Low |
| Security | Isolated service account | Direct access |
| **Persistence** | **✅ Redis-backed** | **❌ None** |
| **Survives Restarts** | **✅ Yes** | **N/A** |

## Future Enhancements

- [ ] Metrics export (Prometheus)
- [ ] GraphQL API
- [ ] Persistent storage option
- [ ] Multi-cluster support
- [ ] Advanced filtering and search
- [ ] WebSocket support for real-time updates
- [ ] Helm release metadata extraction
- [ ] Custom resource definitions (CRDs) support

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

[Add your license here]

## Credits

Created as part of the Grafana Kubernetes visualization project.
