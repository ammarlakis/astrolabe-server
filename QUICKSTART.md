# Astrolabe Quick Start Guide

Get Astrolabe up and running in 5 minutes!

## Prerequisites

- Kubernetes cluster (1.24+)
- `kubectl` configured
- Go 1.21+ (for local development)

## Option 1: Deploy to Kubernetes (Recommended)

### Step 1: Build and Deploy

```bash
# Navigate to the project directory
cd kubernetes-state-server

# Build the Docker image
make docker-build

# Deploy to your cluster
kubectl apply -f deploy/deployment.yaml
```

### Step 2: Verify Deployment

```bash
# Check if pod is running
kubectl -n astrolabe-system get pods

# Expected output:
# NAME                         READY   STATUS    RESTARTS   AGE
# astrolabe-xxxxxxxxxx-xxxxx   1/1     Running   0          30s

# Check logs
kubectl -n astrolabe-system logs -l app=astrolabe --tail=20
```

### Step 3: Test the API

```bash
# Port-forward to access the API
kubectl -n astrolabe-system port-forward svc/astrolabe 8080:8080 &

# Test health endpoint
curl http://localhost:8080/health

# Expected output:
# {"status":"healthy","nodes":42}

# Get all Helm releases
curl http://localhost:8080/api/v1/releases | jq

# Get resources for a specific release
curl "http://localhost:8080/api/v1/resources?release=YOUR_RELEASE_NAME" | jq
```

## Option 2: Run Locally (Development)

### Step 1: Build

```bash
cd kubernetes-state-server
make build
```

### Step 2: Run

```bash
# Run with Helm filter (default)
./bin/astrolabe --in-cluster=false --v=2

# Or run without filters (all resources)
./bin/astrolabe --in-cluster=false --label-selector="" --v=2
```

### Step 3: Test

```bash
# In another terminal
curl http://localhost:8080/health
curl http://localhost:8080/api/v1/releases | jq
```

## Common Commands

### View Logs

```bash
# Follow logs
kubectl -n astrolabe-system logs -l app=astrolabe -f

# View last 100 lines
kubectl -n astrolabe-system logs -l app=astrolabe --tail=100

# Filter for errors
kubectl -n astrolabe-system logs -l app=astrolabe | grep ERROR
```

### Check Resource Usage

```bash
# Memory and CPU usage
kubectl -n astrolabe-system top pod -l app=astrolabe

# Detailed pod info
kubectl -n astrolabe-system describe pod -l app=astrolabe
```

### Update Configuration

```bash
# Edit deployment
kubectl -n astrolabe-system edit deployment astrolabe

# Example: Change label selector
# Find the args section and modify:
#   args:
#     - --label-selector=your-custom-selector

# Restart to apply changes
kubectl -n astrolabe-system rollout restart deployment astrolabe
```

### Uninstall

```bash
kubectl delete -f deploy/deployment.yaml
```

## Next Steps

### 1. Integrate with Grafana Datasource

Update your datasource plugin to use Astrolabe instead of direct Kubernetes API:

```typescript
// Change from:
const url = 'https://kubernetes.default.svc';

// To:
const url = 'http://astrolabe.astrolabe-system.svc.cluster.local:8080';
```

### 2. Expose Externally (Optional)

If Grafana is outside the cluster, expose Astrolabe:

**Option A: Port Forward (Development)**
```bash
kubectl -n astrolabe-system port-forward svc/astrolabe 8080:8080
```

**Option B: Ingress (Production)**
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: astrolabe
  namespace: astrolabe-system
spec:
  rules:
    - host: astrolabe.yourdomain.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: astrolabe
                port:
                  number: 8080
```

### 3. Customize Label Selector

By default, Astrolabe only tracks Helm-managed resources. To change this:

```bash
# Edit deployment
kubectl -n astrolabe-system edit deployment astrolabe

# Modify args:
# For all resources:
args:
  - --label-selector=

# For custom filter:
args:
  - --label-selector=environment=production,team=platform
```

### 4. Monitor Performance

```bash
# Watch resource usage
watch kubectl -n astrolabe-system top pod -l app=astrolabe

# Check graph size
watch 'curl -s http://localhost:8080/health | jq .nodes'
```

## Troubleshooting

### Pod Not Starting

```bash
# Check events
kubectl -n astrolabe-system get events --sort-by='.lastTimestamp'

# Check pod status
kubectl -n astrolabe-system describe pod -l app=astrolabe

# Common issues:
# - Image pull errors: Build and tag the image correctly
# - RBAC errors: Ensure ClusterRole and binding are created
# - Resource limits: Adjust limits in deployment.yaml
```

### No Resources Showing

```bash
# Check label selector
kubectl -n astrolabe-system get deployment astrolabe -o yaml | grep label-selector

# Verify resources have matching labels
kubectl get pods -A -l app.kubernetes.io/managed-by=Helm

# Check logs for errors
kubectl -n astrolabe-system logs -l app=astrolabe | grep -i error
```

### API Not Responding

```bash
# Check if service exists
kubectl -n astrolabe-system get svc astrolabe

# Check if pod is ready
kubectl -n astrolabe-system get pods -l app=astrolabe

# Test from within cluster
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl http://astrolabe.astrolabe-system.svc.cluster.local:8080/health
```

### High Memory Usage

```bash
# Check current usage
kubectl -n astrolabe-system top pod -l app=astrolabe

# Reduce by using label selector
kubectl -n astrolabe-system edit deployment astrolabe
# Add: --label-selector=app.kubernetes.io/managed-by=Helm

# Or increase limits
kubectl -n astrolabe-system edit deployment astrolabe
# Increase resources.limits.memory
```

## Testing with Sample Data

### Install a Test Helm Release

```bash
# Add Bitnami repo
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update

# Install nginx
helm install test-nginx bitnami/nginx -n test --create-namespace

# Wait for deployment
kubectl -n test wait --for=condition=available deployment/test-nginx --timeout=60s

# Query Astrolabe
curl "http://localhost:8080/api/v1/releases" | jq '.[] | select(. == "test-nginx")'
curl "http://localhost:8080/api/v1/resources?release=test-nginx" | jq

# View graph
curl "http://localhost:8080/api/v1/graph?release=test-nginx" | jq

# Cleanup
helm uninstall test-nginx -n test
kubectl delete namespace test
```

## Performance Benchmarks

Expected performance on a typical cluster:

| Cluster Size | Resources Tracked | Memory Usage | CPU Usage | Sync Time |
|--------------|-------------------|--------------|-----------|-----------|
| Small        | 50-100            | ~50 MB       | ~10m      | ~2s       |
| Medium       | 500-1000          | ~150 MB      | ~20m      | ~5s       |
| Large        | 5000-10000        | ~500 MB      | ~50m      | ~15s      |

## What's Next?

- Read the [Architecture Guide](ARCHITECTURE.md) to understand how it works
- Check [Examples](EXAMPLES.md) for advanced usage patterns
- See the full [README](README.md) for complete documentation
- Explore the API endpoints and integrate with your tools

## Getting Help

If you encounter issues:

1. Check the logs: `kubectl -n astrolabe-system logs -l app=astrolabe`
2. Verify RBAC: `kubectl auth can-i list pods --as=system:serviceaccount:astrolabe-system:astrolabe`
3. Test connectivity: Port-forward and curl the health endpoint
4. Review the troubleshooting section above

Happy graphing! 🚀
