# Astrolabe Persistence Guide

## Overview

Astrolabe now supports **persistent storage** using Redis, making it production-ready and resilient to pod restarts. The graph data is stored both in-memory (for fast queries) and in Redis (for durability).

## Architecture

### Hybrid Approach

```
┌─────────────────────────────────────────────────────────┐
│                    Astrolabe Pod                         │
│                                                          │
│  ┌──────────────────────────────────────────────────┐  │
│  │           In-Memory Graph (Fast Reads)            │  │
│  │  - O(1) lookups by UID                            │  │
│  │  - Indexed queries (namespace, kind, labels)      │  │
│  │  - Sub-millisecond response times                 │  │
│  └───────────────────┬──────────────────────────────┘  │
│                      │                                   │
│                      │ Async Writes                      │
│                      ▼                                   │
│  ┌──────────────────────────────────────────────────┐  │
│  │         Persistence Layer (Redis Client)          │  │
│  │  - Batched writes (100 ops)                       │  │
│  │  - Periodic flush (30s)                           │  │
│  │  - Graceful shutdown snapshot                     │  │
│  └───────────────────┬──────────────────────────────┘  │
│                      │                                   │
└──────────────────────┼───────────────────────────────────┘
                       │
                       │ TCP Connection
                       ▼
┌─────────────────────────────────────────────────────────┐
│                    Redis Server                          │
│  ┌──────────────────────────────────────────────────┐  │
│  │              In-Memory Data Store                 │  │
│  │  - Nodes: astrolabe:node:<UID>                    │  │
│  │  - Edges: astrolabe:edge:<from>:<to>              │  │
│  │  - Indexes: astrolabe:index:*                     │  │
│  └───────────────────┬──────────────────────────────┘  │
│                      │                                   │
│                      │ Persistence                       │
│                      ▼                                   │
│  ┌──────────────────────────────────────────────────┐  │
│  │           Disk Storage (RDB + AOF)                │  │
│  │  - RDB: Snapshots every 5 min                     │  │
│  │  - AOF: Append-only file (fsync every second)     │  │
│  │  - Survives Redis restarts                        │  │
│  └──────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

### Data Flow

**On Resource Update:**
1. Kubernetes informer receives event
2. Processor updates in-memory graph (immediate)
3. Change queued for async write to Redis
4. Batch written to Redis every 30s or when 100 ops accumulated

**On Pod Restart:**
1. Astrolabe starts, connects to Redis
2. Loads entire graph from Redis (~1-5 seconds)
3. Rebuilds in-memory indexes
4. Starts informers (receives updates for any changes since load)
5. Ready to serve requests

## Configuration

### Environment Variables

```bash
# Enable persistence
ENABLE_PERSISTENCE=true

# Redis connection
REDIS_ADDR=redis:6379
REDIS_PASSWORD=          # Optional
REDIS_DB=0               # Database number (0-15)
```

### Command-Line Flags

```bash
astrolabe \
  --enable-persistence=true \
  --redis-addr=localhost:6379 \
  --redis-password="" \
  --redis-db=0 \
  --snapshot-interval=300    # Periodic snapshot interval in seconds
```

## Deployment Options

### Option 1: Docker Compose (Development)

```bash
# Start Redis and Astrolabe
docker-compose up -d

# View logs
docker-compose logs -f astrolabe

# Stop
docker-compose down
```

The `docker-compose.yaml` includes:
- Redis with persistence configured
- Astrolabe with persistence enabled
- Volume for Redis data
- Health checks

### Option 2: Kubernetes with Redis

#### Deploy Redis

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: redis-data
  namespace: astrolabe-system
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
  namespace: astrolabe-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: redis
  template:
    metadata:
      labels:
        app: redis
    spec:
      containers:
        - name: redis
          image: redis:7-alpine
          args:
            - redis-server
            - --appendonly
            - "yes"
            - --save
            - "900 1"
            - --save
            - "300 10"
            - --save
            - "60 10000"
          ports:
            - containerPort: 6379
          volumeMounts:
            - name: data
              mountPath: /data
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: redis-data
---
apiVersion: v1
kind: Service
metadata:
  name: redis
  namespace: astrolabe-system
spec:
  selector:
    app: redis
  ports:
    - port: 6379
      targetPort: 6379
```

#### Update Astrolabe Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: astrolabe
  namespace: astrolabe-system
spec:
  template:
    spec:
      containers:
        - name: astrolabe
          env:
            - name: ENABLE_PERSISTENCE
              value: "true"
            - name: REDIS_ADDR
              value: "redis:6379"
            - name: REDIS_DB
              value: "0"
```

### Option 3: External Redis (Production)

For production, use a managed Redis service:

**AWS ElastiCache:**
```bash
REDIS_ADDR=my-cluster.abc123.0001.use1.cache.amazonaws.com:6379
ENABLE_PERSISTENCE=true
```

**Google Cloud Memorystore:**
```bash
REDIS_ADDR=10.0.0.3:6379
ENABLE_PERSISTENCE=true
```

**Azure Cache for Redis:**
```bash
REDIS_ADDR=my-cache.redis.cache.windows.net:6379
REDIS_PASSWORD=<access-key>
ENABLE_PERSISTENCE=true
```

## Redis Configuration

### Persistence Settings

The included `redis.conf` configures:

**RDB (Snapshots):**
```
save 900 1      # After 15 min if ≥1 key changed
save 300 10     # After 5 min if ≥10 keys changed
save 60 10000   # After 1 min if ≥10000 keys changed
```

**AOF (Append-Only File):**
```
appendonly yes
appendfsync everysec    # Fsync every second
aof-use-rdb-preamble yes
```

### Memory Management

```
maxmemory 512mb
maxmemory-policy allkeys-lru
```

Adjust based on your cluster size:
- Small (< 1000 resources): 256 MB
- Medium (1000-5000 resources): 512 MB
- Large (5000-20000 resources): 1-2 GB

## Performance

### Write Performance

**Async Writes:**
- Batched in groups of 100 operations
- Flushed every 30 seconds
- Non-blocking (doesn't slow down queries)

**Throughput:**
- ~3,000-5,000 writes/second to Redis
- Handles bursts during initial sync

### Read Performance

**In-Memory Queries:**
- No Redis overhead for reads
- Same performance as non-persistent mode
- Sub-millisecond response times

**Startup Time:**
- Small cluster (< 1000 resources): ~1-2 seconds
- Medium cluster (1000-5000 resources): ~3-5 seconds
- Large cluster (5000-20000 resources): ~10-15 seconds

### Storage Requirements

**Redis Memory:**
- ~1 KB per node (resource)
- ~100 bytes per edge
- Example: 5000 resources with 15000 edges = ~20 MB

**Disk Space (RDB + AOF):**
- Compressed RDB: ~50% of memory
- AOF: 2-3x memory (before rewrite)
- Example: 20 MB memory = ~10 MB RDB + ~50 MB AOF

## Monitoring

### Health Checks

```bash
# Check if persistence is enabled
curl http://localhost:8080/health

# Check Redis connection
redis-cli -h localhost ping

# Check Redis memory usage
redis-cli -h localhost INFO memory

# Check number of keys
redis-cli -h localhost DBSIZE
```

### Logs

```bash
# Astrolabe logs
kubectl -n astrolabe-system logs -l app=astrolabe | grep -i redis

# Look for:
# - "Successfully connected to Redis"
# - "Loaded X nodes from Redis"
# - "Snapshot completed in Xms"
```

### Metrics

Key metrics to monitor:
- Redis memory usage
- Redis CPU usage
- Number of keys in Redis
- Astrolabe startup time
- Write batch sizes

## Backup and Recovery

### Manual Backup

```bash
# Trigger RDB snapshot
redis-cli BGSAVE

# Copy RDB file
kubectl cp astrolabe-system/redis-pod:/data/dump.rdb ./backup/dump.rdb

# Copy AOF file
kubectl cp astrolabe-system/redis-pod:/data/appendonly.aof ./backup/appendonly.aof
```

### Automated Backups

Use Kubernetes CronJob:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: redis-backup
  namespace: astrolabe-system
spec:
  schedule: "0 */6 * * *"  # Every 6 hours
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: backup
              image: redis:7-alpine
              command:
                - /bin/sh
                - -c
                - |
                  redis-cli -h redis BGSAVE
                  sleep 10
                  tar -czf /backup/redis-$(date +%Y%m%d-%H%M%S).tar.gz /data
          restartPolicy: OnFailure
```

### Recovery

```bash
# Stop Astrolabe
kubectl -n astrolabe-system scale deployment astrolabe --replicas=0

# Stop Redis
kubectl -n astrolabe-system scale deployment redis --replicas=0

# Restore RDB file
kubectl cp ./backup/dump.rdb astrolabe-system/redis-pod:/data/dump.rdb

# Start Redis
kubectl -n astrolabe-system scale deployment redis --replicas=1

# Start Astrolabe (will load from Redis)
kubectl -n astrolabe-system scale deployment astrolabe --replicas=1
```

## Troubleshooting

### Persistence Not Working

```bash
# Check if persistence is enabled
kubectl -n astrolabe-system logs -l app=astrolabe | grep "Persistence enabled"

# Check Redis connection
kubectl -n astrolabe-system logs -l app=astrolabe | grep "Redis"

# Verify Redis is running
kubectl -n astrolabe-system get pods -l app=redis
```

### Slow Startup

```bash
# Check number of keys being loaded
redis-cli DBSIZE

# Check Astrolabe logs for load time
kubectl -n astrolabe-system logs -l app=astrolabe | grep "loaded from Redis"

# If too slow, consider:
# - Increasing Redis resources
# - Using Redis Cluster for sharding
# - Reducing label selector scope
```

### High Memory Usage

```bash
# Check Redis memory
redis-cli INFO memory

# Check for memory leaks
redis-cli MEMORY DOCTOR

# Reduce memory:
# - Set maxmemory limit
# - Use allkeys-lru eviction
# - Reduce snapshot frequency
```

### Data Loss

```bash
# Check if RDB/AOF files exist
kubectl exec -it redis-pod -- ls -lh /data

# Check Redis persistence config
redis-cli CONFIG GET save
redis-cli CONFIG GET appendonly

# Verify writes are persisting
redis-cli LASTSAVE
```

## Best Practices

### Production Deployment

1. **Use Managed Redis**: AWS ElastiCache, Google Memorystore, Azure Cache
2. **Enable Both RDB and AOF**: Maximum durability
3. **Regular Backups**: Automated daily backups
4. **Monitor Memory**: Set alerts for 80% usage
5. **Use Persistent Volumes**: For self-hosted Redis

### High Availability

1. **Redis Sentinel**: For automatic failover
2. **Redis Cluster**: For horizontal scaling
3. **Multiple Astrolabe Replicas**: Load balancing
4. **Health Checks**: Liveness and readiness probes

### Security

1. **Enable Redis AUTH**: Set password
2. **Network Policies**: Restrict Redis access
3. **TLS/SSL**: Encrypt Redis connections
4. **Backup Encryption**: Encrypt backup files

## Migration

### From In-Memory to Persistent

1. Deploy Redis
2. Update Astrolabe deployment with persistence flags
3. Restart Astrolabe (will start populating Redis)
4. Verify data in Redis: `redis-cli DBSIZE`

### From Persistent to In-Memory

1. Update Astrolabe deployment: `ENABLE_PERSISTENCE=false`
2. Restart Astrolabe
3. Optionally remove Redis deployment

## Performance Tuning

### Redis Tuning

```conf
# Increase max clients
maxclients 10000

# Disable slow commands
rename-command FLUSHDB ""
rename-command FLUSHALL ""

# Optimize for writes
appendfsync everysec
no-appendfsync-on-rewrite yes

# Memory optimization
activerehashing yes
lazyfree-lazy-eviction yes
```

### Astrolabe Tuning

```bash
# Increase snapshot interval (less frequent snapshots)
--snapshot-interval=600  # 10 minutes

# Disable periodic snapshots (only on shutdown)
--snapshot-interval=0

# Increase batch size (fewer Redis round-trips)
# Edit pkg/graph/persistent.go: batchSize := 200
```

## FAQ

**Q: Does persistence slow down queries?**
A: No, queries are served from in-memory graph. Redis is only used for writes.

**Q: What happens if Redis goes down?**
A: Astrolabe continues serving from in-memory cache. New updates won't persist until Redis recovers.

**Q: Can I use Redis Cluster?**
A: Yes, but you'll need to modify the Redis client configuration to support cluster mode.

**Q: How much does persistence cost?**
A: Minimal. Redis memory ~= 10-20% of Astrolabe memory. Managed Redis starts at ~$15/month.

**Q: Can I disable persistence after enabling it?**
A: Yes, just set `ENABLE_PERSISTENCE=false` and restart. Data in Redis remains but won't be used.

**Q: Is the data encrypted?**
A: Redis data is not encrypted by default. Use Redis TLS and encrypt backups for sensitive data.
