package config

import (
	"os"

	"github.com/kelseyhightower/envconfig"
)

// Worker holds worker node configuration.
type Worker struct {
	// ID is the unique worker identifier (defaults to hostname).
	ID string `envconfig:"WORKER_ID"`

	// Address is the gRPC listen address.
	Address string `envconfig:"WORKER_ADDRESS" default:":50052"`

	// MasterAddress is the master node address.
	MasterAddress string `envconfig:"MASTER_ADDRESS" required:"true"`

	// MaxConcurrentTasks is the max parallel tasks.
	MaxConcurrentTasks int `envconfig:"MAX_CONCURRENT_TASKS" default:"10"`

	// HeartbeatInterval is seconds between heartbeats.
	HeartbeatInterval int `envconfig:"HEARTBEAT_INTERVAL_SECONDS" default:"5"`

	// LogLevel is the logging level.
	LogLevel string `envconfig:"LOG_LEVEL" default:"info"`
}

// LoadWorker loads worker configuration from environment variables.
func LoadWorker() (Worker, error) {
	var cfg Worker
	err := envconfig.Process("", &cfg)
	if err != nil {
		return cfg, err
	}

	// Default worker ID to hostname
	if cfg.ID == "" {
		hostname, _ := os.Hostname()
		if hostname != "" {
			cfg.ID = hostname
		} else {
			cfg.ID = "worker-1"
		}
	}

	return cfg, nil
}
