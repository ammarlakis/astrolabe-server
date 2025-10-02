# Changelog

All notable changes to the Astrolabe project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Planned
- Prometheus metrics export
- WebSocket support for real-time updates
- GraphQL API
- Unit and integration tests
- Persistent storage option
- Multi-cluster support

## [0.1.0] - 2025-10-02

### Added - Initial Release

#### Core Features
- In-memory graph database for Kubernetes resources
- Real-time resource watching via Kubernetes informers
- Automatic relationship (edge) derivation between resources
- HTTP RESTful API for querying the graph
- Helm-aware resource tracking
- Label-based resource filtering

#### Resource Types
- **Core**: Pods, Services, ServiceAccounts, ConfigMaps, Secrets, PVCs, PVs, Namespaces
- **Workloads**: Deployments, StatefulSets, DaemonSets, ReplicaSets, Jobs, CronJobs
- **Networking**: Ingresses, EndpointSlices
- **Storage**: StorageClasses
- **Autoscaling**: HorizontalPodAutoscalers
- **Policy**: PodDisruptionBudgets

#### Edge Types
- `owns` - Ownership relationships (ownerReferences)
- `selects` - Service to Pod selection
- `endpoints` - Service to EndpointSlice
- `routes-to` - Ingress to Service backends
- `mounts` - Pod to PVC volume mounts
- `binds` - PVC to PV bindings
- `uses-configmap` - ConfigMap references
- `uses-secret` - Secret references
- `uses-sa` - ServiceAccount usage
- `scales` - HPA to scale target

#### API Endpoints
- `GET /health` - Health check
- `GET /api/v1/resources` - Query resources with filters
- `GET /api/v1/releases` - List Helm releases
- `GET /api/v1/charts` - List Helm charts
- `GET /api/v1/namespaces` - List namespaces
- `GET /api/v1/graph` - Get graph data with nodes and edges

#### Deployment
- Kubernetes manifests for in-cluster deployment
- RBAC configuration with minimal read-only permissions
- Dockerfile with multi-stage build
- Makefile for build automation

#### Documentation
- Comprehensive README with installation and usage
- Architecture documentation with design details
- Examples and usage patterns
- Quick start guide
- Project summary

#### Performance
- Efficient indexing for O(1) lookups
- Shared informers to minimize cluster load
- Label filtering to reduce memory footprint
- Concurrent processing during startup
- Memory usage: ~50-500 MB depending on cluster size
- CPU usage: ~10-50m in steady state

#### Configuration
- Command-line flags for customization
- In-cluster and local development modes
- Configurable label selectors
- Adjustable log verbosity

### Technical Details

#### Dependencies
- Go 1.21+
- Kubernetes client-go v0.28.4
- Standard library for HTTP server

#### Architecture
- Event-driven architecture with informers
- Processor pattern for resource handling
- Thread-safe graph with RWMutex
- Multiple indexes for query optimization

#### Security
- Read-only ClusterRole
- Dedicated ServiceAccount
- No authentication (network-level security)
- Minimal privileges

---

## Version History

### Version Numbering
- **Major version** (X.0.0): Breaking API changes
- **Minor version** (0.X.0): New features, backward compatible
- **Patch version** (0.0.X): Bug fixes, backward compatible

### Release Notes Format
Each release includes:
- **Added**: New features
- **Changed**: Changes to existing functionality
- **Deprecated**: Soon-to-be removed features
- **Removed**: Removed features
- **Fixed**: Bug fixes
- **Security**: Security improvements

---

## Future Roadmap

### v0.2.0 (Planned)
- [ ] Prometheus metrics endpoint
- [ ] Health check improvements
- [ ] Configurable resync period
- [ ] Additional resource types (NetworkPolicies, IngressClasses)
- [ ] Performance optimizations

### v0.3.0 (Planned)
- [ ] WebSocket support for real-time updates
- [ ] GraphQL API
- [ ] Advanced filtering and search
- [ ] Pagination support for large result sets

### v0.4.0 (Planned)
- [ ] Persistent storage option (Redis/PostgreSQL)
- [ ] Resource history tracking
- [ ] Time-series queries

### v1.0.0 (Planned)
- [ ] Production-ready release
- [ ] Comprehensive test coverage
- [ ] Performance benchmarks
- [ ] Security audit
- [ ] Multi-cluster support

---

## Contributing

When contributing, please:
1. Update this CHANGELOG with your changes
2. Follow the format above
3. Add entries under [Unreleased] section
4. Move entries to a version section upon release

## Links
- [Repository](https://github.com/yourusername/astrolabe)
- [Issues](https://github.com/yourusername/astrolabe/issues)
- [Releases](https://github.com/yourusername/astrolabe/releases)
