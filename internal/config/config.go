// Package config provides configuration loading for the distributed command execution system.
package config

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

// Load loads configuration from environment variables into the provided struct.
func Load(prefix string, cfg interface{}) error {
	if err := envconfig.Process(prefix, cfg); err != nil {
		return fmt.Errorf("config load failed: %w", err)
	}
	return nil
}
