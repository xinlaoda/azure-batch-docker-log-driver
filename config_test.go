package main

import (
	"testing"
	"time"
)

func TestParsePluginConfig_Valid(t *testing.T) {
	opts := map[string]string{
		"dce-endpoint":     "https://dce.ingest.monitor.azure.com",
		"dcr-immutable-id": "dcr-abc123",
		"dcr-stream-name":  "Custom-BatchLogs_CL",
	}

	cfg, err := ParsePluginConfig(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DCEEndpoint != "https://dce.ingest.monitor.azure.com" {
		t.Errorf("DCEEndpoint = %q", cfg.DCEEndpoint)
	}
	if cfg.DCRImmutableID != "dcr-abc123" {
		t.Errorf("DCRImmutableID = %q", cfg.DCRImmutableID)
	}
	if cfg.DCRStreamName != "Custom-BatchLogs_CL" {
		t.Errorf("DCRStreamName = %q", cfg.DCRStreamName)
	}
	if cfg.BufferMaxSize != 1000 {
		t.Errorf("BufferMaxSize = %d, want 1000", cfg.BufferMaxSize)
	}
	if cfg.BufferMaxInterval != 5*time.Second {
		t.Errorf("BufferMaxInterval = %v, want 5s", cfg.BufferMaxInterval)
	}
}

func TestParsePluginConfig_CustomBuffer(t *testing.T) {
	opts := map[string]string{
		"dce-endpoint":       "https://dce.ingest.monitor.azure.com",
		"dcr-immutable-id":   "dcr-abc123",
		"dcr-stream-name":    "Custom-BatchLogs_CL",
		"buffer-max-size":    "500",
		"buffer-max-interval": "10s",
	}

	cfg, err := ParsePluginConfig(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BufferMaxSize != 500 {
		t.Errorf("BufferMaxSize = %d, want 500", cfg.BufferMaxSize)
	}
	if cfg.BufferMaxInterval != 10*time.Second {
		t.Errorf("BufferMaxInterval = %v, want 10s", cfg.BufferMaxInterval)
	}
}

func TestParsePluginConfig_MissingRequired(t *testing.T) {
	tests := []struct {
		name string
		opts map[string]string
	}{
		{"missing dce-endpoint", map[string]string{
			"dcr-immutable-id": "x", "dcr-stream-name": "x",
		}},
		{"missing dcr-immutable-id", map[string]string{
			"dce-endpoint": "x", "dcr-stream-name": "x",
		}},
		{"missing dcr-stream-name", map[string]string{
			"dce-endpoint": "x", "dcr-immutable-id": "x",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePluginConfig(tt.opts)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestParsePluginConfig_InvalidBufferSize(t *testing.T) {
	opts := map[string]string{
		"dce-endpoint":     "https://dce.ingest.monitor.azure.com",
		"dcr-immutable-id": "dcr-abc123",
		"dcr-stream-name":  "Custom-BatchLogs_CL",
		"buffer-max-size":  "notanumber",
	}

	_, err := ParsePluginConfig(opts)
	if err == nil {
		t.Error("expected error for invalid buffer-max-size")
	}
}

func TestParsePluginConfig_InvalidBufferInterval(t *testing.T) {
	opts := map[string]string{
		"dce-endpoint":       "https://dce.ingest.monitor.azure.com",
		"dcr-immutable-id":   "dcr-abc123",
		"dcr-stream-name":    "Custom-BatchLogs_CL",
		"buffer-max-interval": "notaduration",
	}

	_, err := ParsePluginConfig(opts)
	if err == nil {
		t.Error("expected error for invalid buffer-max-interval")
	}
}

func TestParsePluginConfig_WithManagedIdentity(t *testing.T) {
	opts := map[string]string{
		"dce-endpoint":              "https://dce.ingest.monitor.azure.com",
		"dcr-immutable-id":          "dcr-abc123",
		"dcr-stream-name":           "Custom-BatchLogs_CL",
		"managed-identity-client-id": "12345678-1234-1234-1234-123456789012",
	}

	cfg, err := ParsePluginConfig(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ManagedIdentityClientID != "12345678-1234-1234-1234-123456789012" {
		t.Errorf("ManagedIdentityClientID = %q", cfg.ManagedIdentityClientID)
	}
}
