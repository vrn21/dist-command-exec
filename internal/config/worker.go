package config

import (
	"os"
	"strings"

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

	// Executors is the executor configuration.
	Executors ExecutorConfig
}

// ExecutorConfig holds configuration for all executors.
type ExecutorConfig struct {
	Command CommandExecutorConfig
}

// CommandExecutorConfig holds configuration for the command executor.
type CommandExecutorConfig struct {
	// Enabled controls whether the command executor is registered.
	Enabled bool `envconfig:"EXECUTOR_COMMAND_ENABLED" default:"true"`

	// AllowedCommands is a comma-separated list of allowed commands.
	// If empty, all commands are allowed (unless blocked).
	AllowedCommands string `envconfig:"EXECUTOR_COMMAND_ALLOWED"`

	// BlockedCommands is a comma-separated list of blocked commands.
	BlockedCommands string `envconfig:"EXECUTOR_COMMAND_BLOCKED"`
}

// AllowedCommandsList returns the allowed commands as a slice.
func (c *CommandExecutorConfig) AllowedCommandsList() []string {
	return splitCSV(c.AllowedCommands)
}

// BlockedCommandsList returns the blocked commands as a slice.
func (c *CommandExecutorConfig) BlockedCommandsList() []string {
	return splitCSV(c.BlockedCommands)
}

// splitCSV splits a comma-separated string into a slice.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
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
