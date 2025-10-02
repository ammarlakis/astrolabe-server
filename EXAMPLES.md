# Astrolabe Usage Examples

## Basic Usage

### 1. Deploy Astrolabe

```bash
# Deploy to Kubernetes
kubectl apply -f deploy/deployment.yaml

# Wait for pod to be ready
kubectl -n astrolabe-system wait --for=condition=ready pod -l app=astrolabe --timeout=60s

# Check logs
kubectl -n astrolabe-system logs -l app=astrolabe -f
```

### 2. Access the API

```bash
# Port-forward to access locally
kubectl -n astrolabe-system port-forward svc/astrolabe 8080:8080

# Test health endpoint
curl http://localhost:8080/health
```

## API Examples

### Get All Helm Releases

```bash
curl http://localhost:8080/api/v1/releases | jq
```

Output:
```json
[
  "prometheus",
  "grafana",
  "nginx-ingress",
  "cert-manager"
]
```

### Get Resources for a Specific Release

```bash
curl "http://localhost:8080/api/v1/resources?release=prometheus" | jq
```

Output:
```json
[
  {
    "name": "prometheus-server",
    "namespace": "monitoring",
    "kind": "Deployment",
    "apiVersion": "apps/v1",
    "status": "Ready",
    "message": "All replicas ready (1/1)",
    "chart": "prometheus-15.10.0",
    "release": "prometheus",
    "age": "5d",
    "replicas": {
      "desired": 1,
      "current": 1,
      "ready": 1,
      "available": 1
    }
  },
  {
    "name": "prometheus-server-7d8f9c5b6-abc12",
    "namespace": "monitoring",
    "kind": "Pod",
    "status": "Ready",
    "message": "Pod is running",
    "chart": "prometheus-15.10.0",
    "release": "prometheus",
    "ownerReferences": [
      {
        "kind": "ReplicaSet",
        "name": "prometheus-server-7d8f9c5b6"
      }
    ],
    "image": "prom/prometheus:v2.40.0",
    "nodeName": "node-1",
    "restartCount": 0
  }
]
```

### Get Resources in a Namespace

```bash
curl "http://localhost:8080/api/v1/resources?namespace=default" | jq
```

### Get Full Graph for a Release

```bash
curl "http://localhost:8080/api/v1/graph?release=nginx-ingress" | jq
```

Output:
```json
{
  "nodes": [
    {
      "uid": "abc-123",
      "name": "nginx-ingress-controller",
      "namespace": "ingress-nginx",
      "kind": "Deployment",
      "status": "Ready",
      "chart": "ingress-nginx-4.0.0",
      "release": "nginx-ingress"
    },
    {
      "uid": "def-456",
      "name": "nginx-ingress-controller-7d8f9c5b6",
      "namespace": "ingress-nginx",
      "kind": "ReplicaSet",
      "status": "Ready"
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

## Advanced Queries

### Find All Pods Using a Specific ConfigMap

```bash
# Get all resources
curl "http://localhost:8080/api/v1/resources" | jq '
  .[] | 
  select(.kind == "Pod") | 
  select(.usedConfigMaps != null) | 
  select(.usedConfigMaps[] | contains("my-config")) |
  {name, namespace, configMaps: .usedConfigMaps}
'
```

### Find All Services Without Endpoints

```bash
curl "http://localhost:8080/api/v1/resources" | jq '
  .[] | 
  select(.kind == "Service") |
  select(.targetPods == null or (.targetPods | length) == 0) |
  {name, namespace, status}
'
```

### Find All Deployments with Unhealthy Replicas

```bash
curl "http://localhost:8080/api/v1/resources" | jq '
  .[] | 
  select(.kind == "Deployment") |
  select(.replicas != null) |
  select(.replicas.ready < .replicas.desired) |
  {name, namespace, replicas}
'
```

### Count Resources by Kind

```bash
curl "http://localhost:8080/api/v1/resources" | jq '
  group_by(.kind) | 
  map({kind: .[0].kind, count: length}) |
  sort_by(.count) |
  reverse
'
```

## Configuration Examples

### Run with Custom Label Selector

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: astrolabe
spec:
  template:
    spec:
      containers:
        - name: astrolabe
          args:
            - --label-selector=environment=production
```

### Run Without Filters (Track All Resources)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: astrolabe
spec:
  template:
    spec:
      containers:
        - name: astrolabe
          args:
            - --label-selector=
```

### Increase Verbosity for Debugging

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: astrolabe
spec:
  template:
    spec:
      containers:
        - name: astrolabe
          args:
            - --v=4  # Debug level logging
```

### Adjust Resource Limits

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: astrolabe
spec:
  template:
    spec:
      containers:
        - name: astrolabe
          resources:
            requests:
              cpu: 200m
              memory: 256Mi
            limits:
              cpu: 1000m
              memory: 1Gi
```

## Integration Examples

### Use with Grafana Datasource

1. **Modify datasource plugin** to use Astrolabe:

```typescript
// In QueryEditor.tsx or datasource.ts
const ASTROLABE_URL = 'http://astrolabe.astrolabe-system.svc.cluster.local:8080';

async function queryResources(release: string, namespace: string) {
  const params = new URLSearchParams();
  if (release) params.append('release', release);
  if (namespace) params.append('namespace', namespace);
  
  const response = await fetch(`${ASTROLABE_URL}/api/v1/resources?${params}`);
  return response.json();
}
```

2. **Update datasource configuration**:

```json
{
  "type": "kubernetes-datasource",
  "url": "http://astrolabe.astrolabe-system.svc.cluster.local:8080",
  "jsonData": {
    "useAstrolabe": true
  }
}
```

### Expose via Ingress

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: astrolabe
  namespace: astrolabe-system
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /
spec:
  rules:
    - host: astrolabe.example.com
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

### Use with kubectl Port-Forward

```bash
# Forward to localhost
kubectl -n astrolabe-system port-forward svc/astrolabe 8080:8080 &

# Use in scripts
RELEASES=$(curl -s http://localhost:8080/api/v1/releases | jq -r '.[]')
for release in $RELEASES; do
  echo "Release: $release"
  curl -s "http://localhost:8080/api/v1/resources?release=$release" | \
    jq -r '.[] | select(.kind == "Pod") | .name'
done
```

## Monitoring Examples

### Check Graph Size

```bash
curl http://localhost:8080/health | jq '.nodes'
```

### Monitor via Logs

```bash
# Watch for errors
kubectl -n astrolabe-system logs -l app=astrolabe -f | grep ERROR

# Watch for specific resource types
kubectl -n astrolabe-system logs -l app=astrolabe -f | grep "Pod"

# Monitor performance
kubectl -n astrolabe-system logs -l app=astrolabe -f | grep "sync"
```

### Resource Usage

```bash
# Check memory usage
kubectl -n astrolabe-system top pod -l app=astrolabe

# Check CPU usage
kubectl -n astrolabe-system top pod -l app=astrolabe --containers
```

## Testing Examples

### Test with Sample Helm Release

```bash
# Install a test release
helm repo add bitnami https://charts.bitnami.com/bitnami
helm install test-nginx bitnami/nginx -n test --create-namespace

# Query Astrolabe
curl "http://localhost:8080/api/v1/releases" | jq '.[] | select(. == "test-nginx")'
curl "http://localhost:8080/api/v1/resources?release=test-nginx" | jq

# Cleanup
helm uninstall test-nginx -n test
```

### Verify Edge Creation

```bash
# Get graph for a release
curl "http://localhost:8080/api/v1/graph?release=test-nginx" | jq '
  {
    nodeCount: (.nodes | length),
    edgeCount: (.edges | length),
    edgeTypes: (.edges | group_by(.type) | map({type: .[0].type, count: length}))
  }
'
```

### Test Label Filtering

```bash
# Deploy with custom label
kubectl run test-pod --image=nginx -l app.kubernetes.io/managed-by=Helm,test=true

# Verify it appears in Astrolabe
curl "http://localhost:8080/api/v1/resources" | jq '.[] | select(.name == "test-pod")'

# Cleanup
kubectl delete pod test-pod
```

## Troubleshooting Examples

### Debug Missing Resources

```bash
# Check if resource exists in cluster
kubectl get pods -A -l app.kubernetes.io/managed-by=Helm

# Check Astrolabe logs for that resource
kubectl -n astrolabe-system logs -l app=astrolabe | grep "pod-name"

# Verify label selector
kubectl -n astrolabe-system get deployment astrolabe -o yaml | grep label-selector
```

### Debug Missing Edges

```bash
# Get full graph
curl "http://localhost:8080/api/v1/graph" > graph.json

# Check specific node's edges
jq '.edges[] | select(.from == "uid-here" or .to == "uid-here")' graph.json

# Verify owner references in Kubernetes
kubectl get pod pod-name -o jsonpath='{.metadata.ownerReferences}'
```

### Performance Issues

```bash
# Check cache sync time
kubectl -n astrolabe-system logs -l app=astrolabe | grep "sync"

# Check for errors
kubectl -n astrolabe-system logs -l app=astrolabe | grep -i error

# Restart to clear cache
kubectl -n astrolabe-system rollout restart deployment astrolabe
```
