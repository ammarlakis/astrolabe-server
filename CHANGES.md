# Recent Changes Summary

## Issue: PersistentVolumes Not Appearing in Release Queries

### Root Causes Identified

1. **PV Informer Filtered by Label Selector**
   - PersistentVolumes are cluster-scoped and typically don't have Helm labels
   - The informer factory was applying `app.kubernetes.io/managed-by=Helm` selector to ALL resource types
   - PVs were never entering the graph

2. **PVC Metadata Not Populated**
   - `PVCProcessor` was creating nodes but not setting `node.Metadata.VolumeName`
   - This prevented the fallback lookup mechanism in `includePersistentVolumes()`

3. **No PV Inclusion Logic**
   - API server had no mechanism to add PVs when returning release resources
   - PVs are cluster-scoped, so they don't appear in `GetNodesByHelmRelease()` results

### Fixes Applied

#### 1. PV Informer Bypass (`pkg/informers/manager.go`)

Created a separate informer factory without label selector specifically for PVs:

```go
type Manager struct {
    factory        informers.SharedInformerFactory  // With label selector
    extraFactories []informers.SharedInformerFactory // Without selector (for PVs)
    // ...
}

func (m *Manager) registerPVInformer() {
    var informer cache.SharedIndexInformer
    if m.labelSelector != "" {
        // Create extra factory without selector for PVs
        extraFactory := informers.NewSharedInformerFactory(m.clientset, defaultResyncPeriod)
        m.extraFactories = append(m.extraFactories, extraFactory)
        informer = extraFactory.Core().V1().PersistentVolumes().Informer()
    } else {
        informer = m.factory.Core().V1().PersistentVolumes().Informer()
    }
    // ... register handlers
}
```

#### 2. PVC Metadata Population (`pkg/processors/core.go`)

Already present - no changes needed:

```go
func (p *PVCProcessor) Process(obj interface{}, eventType EventType) error {
    // ...
    node.Metadata = &graph.ResourceMetadata{
        VolumeName: pvc.Spec.VolumeName,  // ✓ Already set
    }
    // ...
}
```

#### 3. Graph Lookup for Cluster-Scoped Resources (`pkg/graph/types.go`)

Normalize empty namespace to `_cluster` key for consistent indexing:

```go
func (g *Graph) GetNodesByNamespaceKind(namespace, kind string) []*Node {
    g.mu.RLock()
    defer g.mu.RUnlock()

    nsKey := namespace
    if nsKey == "" {
        nsKey = "_cluster"  // Cluster-scoped resources
    }

    if kindMap, exists := g.byNamespaceKind[nsKey]; exists {
        if nodes, exists := kindMap[kind]; exists {
            // Return copy
            result := make([]*Node, len(nodes))
            copy(result, nodes)
            return result
        }
    }
    return nil
}
```

#### 4. PV Inclusion Helper (`pkg/api/server.go`)

Added function to append PVs bound to PVCs in the result set:

```go
func (s *Server) includePersistentVolumes(nodes []*graph.Node, releaseName string) []*graph.Node {
    // For each PVC in the node set:
    // 1. Skip if it doesn't belong to the requested release
    // 2. Find bound PV via edge or volumeName lookup
    // 3. Add PV to result set
    
    for i := 0; i < initialLen; i++ {
        node := nodes[i]
        if strings.ToLower(node.Kind) != "persistentvolumeclaim" {
            continue
        }

        // Skip PVCs that don't belong to the requested release
        if releaseName != "" && node.HelmRelease != releaseName {
            continue  // ← KEY FIX: prevents zigbee2mqtt PVC from adding its PV
        }

        // Try edge first, fallback to volumeName lookup
        // ...
    }
}
```

## Issue: zigbee2mqtt Resources Leaking into homeassistant Release

### Root Cause

The `zigbee2mqtt` StatefulSet and PVC exist in the `homeassistant` namespace but have **no Helm release labels** (they're unmanaged or from a different deployment method).

**Graph traversal path**:
```
homeassistant PVC (release=homeassistant)
  → homeassistant PV (no release, cluster-scoped)
    ← zigbee2mqtt PVC (no release, in homeassistant namespace) [reverse binding edge]
      ← zigbee2mqtt Pod (no release)
```

When `expandRelatedNodes()` traversed from the PV, it found the zigbee2mqtt PVC via the reverse edge. Since the PVC had no release tag, it passed the original filter.

### Fix Applied

Modified `expandRelatedNodes()` to track which nodes belong to the requested release and only include unmanaged resources if they're **directly connected** to a release resource:

```go
func (s *Server) expandRelatedNodes(base []*graph.Node, namespace string, releaseName string) []*graph.Node {
    // Track nodes that belong to the requested release
    releaseNodes := make(map[types.UID]bool)
    for _, node := range base {
        if releaseName != "" && node.HelmRelease == releaseName {
            releaseNodes[node.UID] = true
        }
    }

    // During traversal:
    for _, neighbour := range neighbours {
        // For unmanaged resources, only include if directly connected to a release resource
        if releaseName != "" && neighbour.HelmRelease == "" {
            if !releaseNodes[current.UID] {
                continue  // ← Prevents multi-hop traversal through unmanaged resources
            }
        }

        // Skip resources from other releases
        if releaseName != "" && neighbour.HelmRelease != "" && neighbour.HelmRelease != releaseName {
            continue
        }
        
        // ... add to result
    }
}
```

**Result**:
- homeassistant PVC → homeassistant PV ✓ (direct connection, PV has no release)
- homeassistant PV → zigbee2mqtt PVC ✗ (PV is not a release node, can't expand to unmanaged PVC)

## Testing

Rebuild and restart:
```bash
docker-compose -f docker-compose.integration.yml build astrolabe
docker-compose -f docker-compose.integration.yml up -d
```

Verify PVs appear:
```bash
curl -s 'http://localhost:8080/api/v1/resources?release=homeassistant' \
  | jq '.[] | select(.kind=="PersistentVolume")'
```

Verify zigbee2mqtt doesn't leak:
```bash
curl -s 'http://localhost:8080/api/v1/graph?release=homeassistant' \
  | jq '.nodes[] | select(.name | contains("zigbee"))'
```

Should return empty (no zigbee2mqtt resources).

## Files Modified

1. `pkg/informers/manager.go` - PV informer bypass
2. `pkg/graph/types.go` - Cluster-scoped namespace normalization
3. `pkg/api/server.go` - PV inclusion + release filtering improvements
4. `API_FILTERING.md` - Documentation of filtering logic (new)
5. `CHANGES.md` - This file (new)
