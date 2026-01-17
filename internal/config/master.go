package config

import (
	"github.com/kelseyhightower/envconfig"
)

// Master holds master node configuration.
type Master struct {
	// Address is the gRPC listen address.
	Address string `envconfig:"MASTER_ADDRESS" default:":50051"`

	// MaxConcurrentJobs is the maximum queue size.
	MaxConcurrentJobs int `envconfig:"MAX_CONCURRENT_JOBS" default:"10000"`

	// HeartbeatTimeout is seconds before marking a node unhealthy.
	HeartbeatTimeout int `envconfig:"HEARTBEAT_TIMEOUT_SECONDS" default:"15"`

	// LogLevel is the logging level.
	LogLevel string `envconfig:"LOG_LEVEL" default:"info"`

	// ScalingEnabled enables K8s auto-scaling.
	ScalingEnabled bool `envconfig:"SCALING_ENABLED" default:"false"`

	// K8sNamespace is the Kubernetes namespace.
	K8sNamespace string `envconfig:"K8S_NAMESPACE" default:"default"`

	// WorkerDeployment is the worker deployment name.
	WorkerDeployment string `envconfig:"WORKER_DEPLOYMENT" default:"dist-exec-worker"`
}

// LoadMaster loads master configuration from environment variables.
func LoadMaster() (Master, error) {
	var cfg Master
	err := envconfig.Process("", &cfg)
	return cfg, err
}
