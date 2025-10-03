package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ammarlakis/astrolabe/pkg/api"
	"github.com/ammarlakis/astrolabe/pkg/graph"
	"github.com/ammarlakis/astrolabe/pkg/informers"
	"github.com/ammarlakis/astrolabe/pkg/storage"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var (
	kubeconfig        string
	port              int
	labelSelector     string
	inCluster         bool
	enablePersistence bool
	redisAddr         string
	redisPassword     string
	redisDB           int
	snapshotInterval  int
)

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (optional, uses in-cluster config if not set)")
	flag.IntVar(&port, "port", 8080, "HTTP API server port")
	flag.StringVar(&labelSelector, "label-selector", "", "Label selector to filter resources (empty for all resources)")
	flag.BoolVar(&inCluster, "in-cluster", true, "Use in-cluster configuration")
	flag.BoolVar(&enablePersistence, "enable-persistence", getEnvBool("ENABLE_PERSISTENCE", false), "Enable Redis persistence")
	flag.StringVar(&redisAddr, "redis-addr", getEnv("REDIS_ADDR", "localhost:6379"), "Redis address")
	flag.StringVar(&redisPassword, "redis-password", getEnv("REDIS_PASSWORD", ""), "Redis password")
	flag.IntVar(&redisDB, "redis-db", getEnvInt("REDIS_DB", 0), "Redis database number")
	flag.IntVar(&snapshotInterval, "snapshot-interval", 300, "Snapshot interval in seconds (0 to disable periodic snapshots)")

	klog.InitFlags(nil)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1"
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intValue int
		fmt.Sscanf(value, "%d", &intValue)
		return intValue
	}
	return defaultValue
}

func main() {
	flag.Parse()

	klog.Info("Starting Astrolabe - Kubernetes State Server")

	// Check for environment variable override for label selector
	if envSelector := os.Getenv("LABEL_SELECTOR"); envSelector != "" || os.Getenv("LABEL_SELECTOR") == "" {
		// If LABEL_SELECTOR env var is explicitly set (even to empty), use it
		if _, exists := os.LookupEnv("LABEL_SELECTOR"); exists {
			labelSelector = envSelector
		}
	}

	if labelSelector == "" {
		klog.Info("Label selector: <empty> (watching ALL resources)")
	} else {
		klog.Infof("Label selector: %s", labelSelector)
	}
	klog.Infof("API port: %d", port)

	// Create Kubernetes client
	config, err := getKubeConfig()
	if err != nil {
		klog.Fatalf("Failed to get Kubernetes config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create Kubernetes clientset: %v", err)
	}

	// Test connection
	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		klog.Fatalf("Failed to connect to Kubernetes cluster: %v", err)
	}
	klog.Infof("Connected to Kubernetes cluster version: %s", serverVersion.GitVersion)

	var g graph.GraphInterface
	var persistentGraph *graph.PersistentGraph

	if enablePersistence {
		klog.Infof("Persistence enabled - connecting to Redis at %s", redisAddr)

		// Create Redis backend
		redisStore, err := storage.NewRedisStore(redisAddr, redisPassword, redisDB)
		if err != nil {
			klog.Fatalf("Failed to create Redis store: %v", err)
		}
		defer redisStore.Close()

		// Create persistent graph with async writes for better performance
		persistentGraph = graph.NewPersistentGraph(redisStore, true)
		g = persistentGraph

		// Load existing graph from Redis
		if err := persistentGraph.LoadFromBackend(); err != nil {
			klog.Warningf("Failed to load graph from Redis (starting fresh): %v", err)
		}

		klog.Info("Initialized persistent graph with Redis backend")
	} else {
		klog.Info("Persistence disabled - using in-memory only graph")
		g = graph.NewGraph()
	}

	// Create informer manager
	manager := informers.NewManager(clientset, g, labelSelector)

	// Create API server
	apiServer := api.NewServer(g, port)

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start API server in goroutine
	go func() {
		if err := apiServer.Start(); err != nil {
			klog.Errorf("API server error: %v", err)
			cancel()
		}
	}()

	// Start informers in goroutine
	go func() {
		if err := manager.Start(ctx); err != nil {
			klog.Errorf("Informer manager error: %v", err)
			cancel()
		}
	}()

	// Start periodic snapshot if enabled
	if enablePersistence && persistentGraph != nil && snapshotInterval > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(snapshotInterval) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					klog.V(2).Info("Creating periodic snapshot...")
					if err := persistentGraph.Snapshot(); err != nil {
						klog.Errorf("Failed to create snapshot: %v", err)
					}
				case <-ctx.Done():
					return
				}
			}
		}()
		klog.Infof("Periodic snapshots enabled (interval: %ds)", snapshotInterval)
	}

	klog.Info("Astrolabe is running. Press Ctrl+C to exit.")

	// Wait for signal
	select {
	case <-sigCh:
		klog.Info("Received shutdown signal")
	case <-ctx.Done():
		klog.Info("Context cancelled")
	}

	// Graceful shutdown
	klog.Info("Shutting down...")
	cancel()

	if err := apiServer.Stop(); err != nil {
		klog.Errorf("Error stopping API server: %v", err)
	}

	// Create final snapshot if persistence is enabled
	if enablePersistence && persistentGraph != nil {
		klog.Info("Creating final snapshot before shutdown...")
		if err := persistentGraph.Snapshot(); err != nil {
			klog.Errorf("Failed to create final snapshot: %v", err)
		}

		// Close persistent graph (flushes pending writes)
		if err := persistentGraph.Close(); err != nil {
			klog.Errorf("Error closing persistent graph: %v", err)
		}
	}

	klog.Info("Shutdown complete")
}

func getKubeConfig() (*rest.Config, error) {
	// Try in-cluster config first if requested
	if inCluster && kubeconfig == "" {
		klog.Info("Using in-cluster Kubernetes configuration")
		config, err := rest.InClusterConfig()
		if err == nil {
			return config, nil
		}
		klog.Warningf("Failed to use in-cluster config: %v", err)
	}

	// Fall back to kubeconfig
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}

	if kubeconfig == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		kubeconfig = homeDir + "/.kube/config"
	}

	klog.Infof("Using kubeconfig: %s", kubeconfig)

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
	}

	return config, nil
}
