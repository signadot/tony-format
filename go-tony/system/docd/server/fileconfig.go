package server

import (
	"fmt"
	"os"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/parse"
)

// Config represents the docd server configuration file structure.
type Config struct {
	// LogdAddr is the address of the logd server to connect to.
	// Can be overridden by CLI flag.
	LogdAddr string `tony:"field=logdAddr"`
}

// LoadConfig loads a configuration file in Tony format.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	node, err := parse.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	cfg := &Config{}
	if err := gomap.FromTonyIR(node, cfg); err != nil {
		return nil, fmt.Errorf("failed to convert config: %w", err)
	}

	return cfg, nil
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		LogdAddr: "localhost:9123",
	}
}
