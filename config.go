package main

import (
	"fmt"
	"strconv"
	"time"
)

// PluginConfig holds the plugin-level configuration set at install time.
type PluginConfig struct {
	DCEEndpoint            string
	DCRImmutableID         string
	DCRStreamName          string
	ManagedIdentityClientID string
	BufferMaxSize          int
	BufferMaxInterval      time.Duration
	Debug                  bool
}

// DefaultPluginConfig returns configuration with sensible defaults.
func DefaultPluginConfig() *PluginConfig {
	return &PluginConfig{
		BufferMaxSize:     1000,
		BufferMaxInterval: 5 * time.Second,
	}
}

// ParsePluginConfig builds PluginConfig from the log-opt map provided by Docker.
func ParsePluginConfig(opts map[string]string) (*PluginConfig, error) {
	cfg := DefaultPluginConfig()

	if v, ok := opts["dce-endpoint"]; ok {
		cfg.DCEEndpoint = v
	}
	if v, ok := opts["dcr-immutable-id"]; ok {
		cfg.DCRImmutableID = v
	}
	if v, ok := opts["dcr-stream-name"]; ok {
		cfg.DCRStreamName = v
	}
	if v, ok := opts["managed-identity-client-id"]; ok {
		cfg.ManagedIdentityClientID = v
	}
	if v, ok := opts["buffer-max-size"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid buffer-max-size %q: %w", v, err)
		}
		cfg.BufferMaxSize = n
	}
	if v, ok := opts["buffer-max-interval"]; ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid buffer-max-interval %q: %w", v, err)
		}
		cfg.BufferMaxInterval = d
	}

	if v, ok := opts["debug"]; ok {
		cfg.Debug = v == "true" || v == "1"
	}

	if cfg.DCEEndpoint == "" {
		return nil, fmt.Errorf("dce-endpoint is required")
	}
	if cfg.DCRImmutableID == "" {
		return nil, fmt.Errorf("dcr-immutable-id is required")
	}
	if cfg.DCRStreamName == "" {
		return nil, fmt.Errorf("dcr-stream-name is required")
	}

	return cfg, nil
}
