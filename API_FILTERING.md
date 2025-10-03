# API Filtering Logic

This document explains how the API server filters and expands resources for different query types.

## Core Filtering Functions

### 1. `expandRelatedNodes(base []*graph.Node, namespace string) []*graph.Node`

**Purpose**: Performs a breadth-first traversal of the graph starting from base nodes to include related resources.

**Algorithm**:
```
1. Start with base nodes (e.g., all resources with release=homeassistant)
2. For each node in the queue:
   a. Get all neighbors (via incoming and outgoing edges)
   b. For each neighbor:
      - Skip if already seen
      - Skip if outside the namespace filter
      - Skip if it has a different Helm release tag than current node
      - Skip if its kind is not in the allowed list
      - Add to result set and queue for further expansion
3. Return all discovered nodes
```

**Allowed Kinds** (for expansion):
- pod
- replicaset
- endpointslice
- configmap
- secret
- serviceaccount
- service
- persistentvolume
- persistentvolumeclaim
- storageclass

**Release Filtering Logic**:
```go
// Skip resources from other Helm releases
if neighbour.HelmRelease != "" && neighbour.HelmRelease != current.HelmRelease {
    continue
}
```

This ensures:
- Resources with **no release tag** (unmanaged) are included
- Resources with **matching release tag** are included
- Resources with **different release tag** are excluded

**Example**:
```
homeassistant Deployment (release=homeassistant)
  → homeassistant ReplicaSet (release=homeassistant) ✓ included
    → homeassistant Pod (release=homeassistant) ✓ included
      → homeassistant PVC (release=homeassistant) ✓ included
        → PV (no release) ✓ included
          → zigbee2mqtt PVC (no release) ✓ included (BUG - should be excluded)
            → zigbee2mqtt Pod (no release) ✓ included (BUG)
```

### 2. `includePersistentVolumes(nodes []*graph.Node, releaseName string) []*graph.Node`

**Purpose**: Adds PersistentVolumes that are bound to PersistentVolumeClaims in the node set.

**Algorithm**:
```
1. Iterate through all nodes in the input set
2. For each PersistentVolumeClaim:
   a. Skip if it doesn't belong to the requested release (when releaseName != "")
   b. Try to find bound PV via edge (PVC → PV binding edge)
   c. Fallback: lookup PV by name from PVC.spec.volumeName
   d. Add PV to result set if found
3. Return nodes with PVs appended
```

**Release Filtering Logic**:
```go
// Skip PVCs that don't belong to the requested release
if releaseName != "" && node.HelmRelease != releaseName {
    continue
}
```

**Why This Matters**:
- PVs are cluster-scoped and typically have no Helm release label
- Without filtering, PVs for **any** PVC in the namespace would be included
- With filtering, only PVs for PVCs **belonging to the requested release** are included

**Example**:
```
Input nodes (release=homeassistant):
- homeassistant PVC (release=homeassistant)
- zigbee2mqtt PVC (no release) ← came from expandRelatedNodes

Without release filter:
- homeassistant PV ✓ (bound to homeassistant PVC)
- zigbee2mqtt PV ✓ (bound to zigbee2mqtt PVC) ← WRONG

With release filter:
- homeassistant PV ✓ (bound to homeassistant PVC)
- zigbee2mqtt PV ✗ (PVC has no release tag, skipped)
```

## API Endpoints

### `/api/v1/resources?release=<name>`

**Flow**:
```
1. Get all nodes with HelmRelease == releaseName
2. Filter by namespace if specified
3. Call includePersistentVolumes(nodes, releaseName)
4. Convert to resource format
5. Return
```

**Does NOT call** `expandRelatedNodes()` - only returns resources directly tagged with the release.

### `/api/v1/graph?release=<name>`

**Flow**:
```
1. Get all nodes with HelmRelease == releaseName
2. Filter by namespace if specified
3. Call expandRelatedNodes(nodes, namespace)
4. Call includePersistentVolumes(nodes, releaseName)
5. Build graph response with nodes and edges
6. Return
```

**Calls both** expansion functions to build a complete graph view.

## Current Issue: zigbee2mqtt Leaking into homeassistant

### Root Cause

The `zigbee2mqtt` PVC exists in the `homeassistant` namespace but has **no Helm release label**:

```bash
$ kubectl get pvc -n homeassistant
NAME                             STATUS   VOLUME
data-zigbee2mqtt-0               Bound    pvc-533573f9-...
homeassistant                    Bound    pvc-0a1cd86d-...
```

**Graph traversal path**:
```
homeassistant PVC (release=homeassistant)
  → homeassistant PV (no release)
    ← zigbee2mqtt PVC (no release) [reverse edge via PVC binding]
      ← zigbee2mqtt Pod (no release)
```

When `expandRelatedNodes()` traverses from the homeassistant PV, it finds the zigbee2mqtt PVC via the reverse binding edge. Since the PVC has **no release tag**, it passes the filter:

```go
if neighbour.HelmRelease != "" && neighbour.HelmRelease != current.HelmRelease {
    continue  // This check doesn't trigger because neighbour.HelmRelease == ""
}
```

### Solution Options

#### Option 1: Stricter Release Filtering (Recommended)

Only include unmanaged resources if they're directly referenced by a managed resource, not discovered via traversal.

```go
// Track which nodes came from the initial release query
initialReleaseNodes := make(map[types.UID]bool)
for _, node := range base {
    initialReleaseNodes[node.UID] = true
}

// During traversal
for _, neighbour := range neighbours {
    // Skip unmanaged resources unless current node is from the release
    if neighbour.HelmRelease == "" && !initialReleaseNodes[current.UID] {
        continue
    }
    
    // Skip resources from other releases
    if neighbour.HelmRelease != "" && neighbour.HelmRelease != current.HelmRelease {
        continue
    }
}
```

#### Option 2: Namespace-Scoped Filtering

Only include resources from the same namespace as the release (excluding cluster-scoped resources like PV).

```go
// During traversal
for _, neighbour := range neighbours {
    // Allow cluster-scoped resources (PV, StorageClass)
    if neighbour.Namespace == "" {
        // ... existing logic
        continue
    }
    
    // For namespaced resources, ensure they belong to the release
    if neighbour.HelmRelease != current.HelmRelease {
        continue
    }
}
```

#### Option 3: PV-Specific Filtering

Don't traverse backwards from PVs to PVCs during expansion.

```go
// During traversal
for _, edge := range current.IncomingEdges {
    // Skip reverse PVC binding edges when expanding from PVs
    if current.Kind == "PersistentVolume" && edge.Type == graph.EdgePVCBinding {
        continue
    }
    
    if neighbour, exists := s.graph.GetNode(edge.FromUID); exists {
        neighbours = append(neighbours, neighbour)
    }
}
```

## Recommended Fix

Implement **Option 1** with a modification to track the "release context" during traversal:

```go
func (s *Server) expandRelatedNodes(base []*graph.Node, namespace string, releaseName string) []*graph.Node {
    // ... existing setup ...
    
    // Track nodes that belong to the release
    releaseNodes := make(map[types.UID]bool)
    for _, node := range base {
        if node.HelmRelease == releaseName {
            releaseNodes[node.UID] = true
        }
    }
    
    for len(queue) > 0 {
        current := queue[0]
        queue = queue[1:]
        
        // ... get neighbours ...
        
        for _, neighbour := range neighbours {
            // ... existing checks ...
            
            // For unmanaged resources, only include if directly connected to release
            if neighbour.HelmRelease == "" {
                if !releaseNodes[current.UID] {
                    continue
                }
            }
            
            // For managed resources, must match release
            if neighbour.HelmRelease != "" && neighbour.HelmRelease != releaseName {
                continue
            }
            
            // ... add to result ...
        }
    }
}
```

This ensures unmanaged resources (like zigbee2mqtt PVC) are only included if they're **directly referenced** by a resource belonging to the release, not discovered via multi-hop traversal through cluster-scoped resources.
