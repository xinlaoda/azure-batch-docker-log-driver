package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

const (
	socketPath = "/run/docker/plugins/batch-log.sock"
	logFile    = "/var/log/azure-batch-log-driver.log"
)

// debugEnabled is set at startup based on the DEBUG env var.
var debugEnabled bool

// debugLog prints a message only when DEBUG mode is on.
func debugLog(format string, args ...any) {
	if debugEnabled {
		log.Printf("[DEBUG] "+format, args...)
	}
}

func main() {
	// Set up file logging (always write to file + stderr)
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Fall back to stderr only if file is not writable
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Printf("WARNING: cannot open log file %s: %v (logging to stderr only)", logFile, err)
	} else {
		multiWriter := io.MultiWriter(os.Stderr, f)
		log.SetOutput(multiWriter)
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	log.Println("azure-batch-docker-log-driver starting...")

	// Parse plugin configuration from environment variables.
	// Docker sets these from the plugin's settings at install/enable time.
	cfg, err := loadConfigFromEnv()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	log.Printf("config: dce=%s dcr=%s stream=%s buffer=%d/%s debug=%v",
		cfg.DCEEndpoint, cfg.DCRImmutableID, cfg.DCRStreamName,
		cfg.BufferMaxSize, cfg.BufferMaxInterval, cfg.Debug)

	debugEnabled = cfg.Debug

	// Create Azure Monitor client
	azClient, err := NewAzureClient(cfg)
	if err != nil {
		log.Fatalf("failed to create azure client: %v", err)
	}

	// Create log buffer
	buffer := NewLogBuffer(azClient, cfg.BufferMaxSize, cfg.BufferMaxInterval)

	// Create driver
	driver := NewDriver(cfg, buffer)

	// Set up HTTP handlers
	mux := http.NewServeMux()

	// Plugin activation endpoint
	mux.HandleFunc("/Plugin.Activate", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"Implements": []string{"LogDriver"},
		})
	})

	// LogDriver endpoints
	mux.HandleFunc("/LogDriver.StartLogging", driver.HandleStartLogging)
	mux.HandleFunc("/LogDriver.StopLogging", driver.HandleStopLogging)
	mux.HandleFunc("/LogDriver.Capabilities", driver.HandleCapabilities)
	mux.HandleFunc("/LogDriver.ReadLogs", driver.HandleReadLogs)

	// Listen on UNIX socket
	os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", socketPath, err)
	}
	defer listener.Close()
	log.Printf("listening on %s", socketPath)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigCh
		log.Println("shutting down...")
		driver.Close()
		buffer.Close()
		listener.Close()
		os.Exit(0)
	}()

	if err := http.Serve(listener, mux); err != nil {
		log.Printf("http server stopped: %v", err)
	}
}

// loadConfigFromEnv reads plugin configuration from environment variables.
// Docker plugin settings are passed as env vars with the setting name uppercased and
// dots replaced with underscores.
func loadConfigFromEnv() (*PluginConfig, error) {
	opts := map[string]string{}

	envMap := map[string]string{
		"DCE_ENDPOINT":              "dce-endpoint",
		"DCR_IMMUTABLE_ID":          "dcr-immutable-id",
		"DCR_STREAM_NAME":           "dcr-stream-name",
		"MANAGED_IDENTITY_CLIENT_ID": "managed-identity-client-id",
		"BUFFER_MAX_SIZE":           "buffer-max-size",
		"BUFFER_MAX_INTERVAL":       "buffer-max-interval",
		"DEBUG":                     "debug",
	}

	for envKey, optKey := range envMap {
		if v := os.Getenv(envKey); v != "" {
			opts[optKey] = v
		}
	}

	cfg, err := ParsePluginConfig(opts)
	if err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}
	return cfg, nil
}
