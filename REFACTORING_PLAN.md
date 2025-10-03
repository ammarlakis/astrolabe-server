# Refactoring Plan

This document outlines the step-by-step plan to split the three main packages into smaller, more maintainable modules.

## Phase 1: Refactor `pkg/api/`

### Step 1.1: Create `pkg/api/interface.go`
Extract the GraphInterface definition:
- Lines 15-22 from `server.go`

### Step 1.2: Create `pkg/api/filtering.go`
Extract graph filtering logic:
- `expandRelatedNodes()` function (lines 24-125)
- `includePersistentVolumes()` function (lines 127-195)

### Step 1.3: Create `pkg/api/responses.go`
Extract response types and conversion functions:
- `Resource` type (line 440+)
- `OwnerReference` type (line 465+)
- `GraphResponse` type (line 580+)
- `NodeResponse` type (line 585+)
- `EdgeResponse` type (line 597+)
- `nodesToResources()` function (line 470+)
- `getRelatedNodeNames()` function (line 555+)
- `getRelatedNodeNamesFromCache()` function (line 567+)
- `buildGraphResponse()` function (line 603+)
- `formatAge()` function (line 642+)

### Step 1.4: Create `pkg/api/handlers.go`
Extract HTTP handlers:
- `handleHealth()` (line 255+)
- `handleResources()` (line 263+)
- `handleReleases()` (line 325+)
- `handleCharts()` (line 351+)
- `handleNamespaces()` (line 378+)
- `handleGraph()` (line 397+)

### Step 1.5: Update `pkg/api/server.go`
Keep only:
- Package imports
- `Server` type definition
- `NewServer()` constructor
- `Start()` method
- `Stop()` method
- `loggingMiddleware()` method

## Phase 2: Refactor `pkg/informers/`

### Step 2.1: Create `pkg/informers/core.go`
Move core resource informers:
- `registerPodInformer()`
- `registerServiceInformer()`
- `registerServiceAccountInformer()`
- `registerConfigMapInformer()`
- `registerSecretInformer()`
- `registerNamespaceInformer()`

### Step 2.2: Create `pkg/informers/workloads.go`
Move workload informers:
- `registerDeploymentInformer()`
- `registerStatefulSetInformer()`
- `registerDaemonSetInformer()`
- `registerReplicaSetInformer()`

### Step 2.3: Create `pkg/informers/batch.go`
Move batch informers:
- `registerJobInformer()`
- `registerCronJobInformer()`

### Step 2.4: Create `pkg/informers/networking.go`
Move networking informers:
- `registerIngressInformer()`
- `registerEndpointSliceInformer()`

### Step 2.5: Create `pkg/informers/storage.go`
Move storage informers:
- `registerPVCInformer()`
- `registerPVInformer()`
- `registerStorageClassInformer()`

### Step 2.6: Create `pkg/informers/autoscaling.go`
Move autoscaling informers:
- `registerHPAInformer()`

### Step 2.7: Create `pkg/informers/policy.go`
Move policy informers:
- `registerPDBInformer()`

### Step 2.8: Update `pkg/informers/manager.go`
Keep only:
- Package imports
- `GraphInterface` definition
- `Manager` type
- `NewManager()` constructor
- `Start()` method
- `Stop()` method
- `registerInformers()` method (calls to individual register functions)
- `waitForCacheSync()` method
- Event handlers: `onAdd()`, `onUpdate()`, `onDelete()`
- `ListPodsBySelector()` utility

## Phase 3: Refactor `pkg/graph/`

### Step 3.1: Keep `pkg/graph/types.go` for type definitions only
- `ResourceStatus` type
- `Node` type
- `ResourceMetadata` type
- `ReplicaInfo` type
- `ObjectReference` type
- `EdgeType` constants
- `Edge` type
- `Graph` type (struct definition only)

### Step 3.2: Create `pkg/graph/graph.go`
Move core graph operations:
- `NewGraph()` constructor
- `AddNode()` method
- `RemoveNode()` method
- `GetNode()` method
- `AddEdge()` method
- `RemoveEdge()` method

### Step 3.3: Create `pkg/graph/indexes.go`
Move index management:
- `addToIndexes()` method
- `removeFromIndexes()` method
- `removeNodeFromSlice()` helper

### Step 3.4: Create `pkg/graph/queries.go`
Move query methods:
- `GetNodesByNamespaceKind()` method
- `GetNodesByHelmRelease()` method
- `GetNodesByLabelSelector()` method
- `GetAllNodes()` method
- `GetAllHelmReleases()` method
- `GetAllHelmCharts()` method
- `intersectNodes()` helper

### Step 3.5: Create `pkg/graph/utils.go`
Move utility functions:
- `NewNodeFromObject()` function

### Step 3.6: Keep `pkg/graph/persistent.go` as-is
Already well-organized.

## Testing After Each Phase

After each phase, run:
```bash
# Build check
go build ./cmd/astrolabe

# Run tests
go test ./...

# Integration test
./test-integration.sh
curl -s 'http://localhost:8080/api/v1/resources?release=homeassistant' | jq '.[] | select(.kind=="PersistentVolume")'
```

## Implementation Order

1. **Phase 1** (`pkg/api/`) - Highest priority, most immediate benefit
2. **Phase 2** (`pkg/informers/`) - Medium priority, easy mechanical split
3. **Phase 3** (`pkg/graph/`) - Lower priority, already fairly organized

## Benefits Summary

### After Refactoring:
- **`pkg/api/`**: 5 files averaging ~130 lines each (vs 1 file with 655 lines)
- **`pkg/informers/`**: 8 files averaging ~60 lines each (vs 1 file with 497 lines)
- **`pkg/graph/`**: 6 files averaging ~85 lines each (vs 2 files with 514+295 lines)

### Code Quality Improvements:
- Single Responsibility Principle - each file has one clear purpose
- Easier navigation - find code by concern, not by scrolling
- Better testability - can test filtering logic independently from HTTP handlers
- Easier onboarding - new developers can understand one file at a time
- Reduced merge conflicts - changes to different concerns touch different files

## Notes

- All functions remain methods on the same structs (no API changes)
- Package-level visibility stays the same
- No changes to external interfaces
- Purely organizational refactoring
- Can be done incrementally (one phase at a time)
