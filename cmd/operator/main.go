package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"dist-command-exec/internal/operator"
)

func main() {
	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Get configuration
	masterAddr := os.Getenv("MASTER_ADDRESS")
	if masterAddr == "" {
		masterAddr = "master:50051"
	}

	namespace := os.Getenv("WATCH_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}

	// Build Kubernetes client
	var config *rest.Config
	var err error

	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath != "" {
		// Out-of-cluster (development)
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	} else {
		// In-cluster
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create k8s config")
	}

	// Create dynamic client
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create dynamic client")
	}

	// Create controller
	controller := operator.NewController(client, masterAddr, namespace)

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info().Msg("shutting down operator")
		cancel()
	}()

	log.Info().
		Str("master", masterAddr).
		Str("namespace", namespace).
		Msg("starting DistributedJob operator")

	// Run controller
	if err := controller.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("controller error")
	}
}
