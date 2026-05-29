package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
)

// Driver implements the Docker LogDriver plugin interface.
type Driver struct {
	mu     sync.RWMutex
	logs   map[string]*LogPair // keyed by FIFO file path
	buffer *LogBuffer
	cfg    *PluginConfig
}

// NewDriver creates a new log driver instance.
func NewDriver(cfg *PluginConfig, buffer *LogBuffer) *Driver {
	return &Driver{
		logs:   make(map[string]*LogPair),
		buffer: buffer,
		cfg:    cfg,
	}
}

// -- Docker LogDriver RPC request/response types --

// StartLoggingRequest is sent by Docker when a container starts.
type StartLoggingRequest struct {
	File string            `json:"File"`
	Info StartLoggingInfo  `json:"Info"`
}

// StartLoggingInfo contains container metadata.
type StartLoggingInfo struct {
	ContainerID        string            `json:"ContainerID"`
	ContainerName      string            `json:"ContainerName"`
	ContainerEntrypoint string           `json:"ContainerEntrypoint"`
	ContainerArgs      []string          `json:"ContainerArgs"`
	ContainerImageID   string            `json:"ContainerImageID"`
	ContainerImageName string            `json:"ContainerImageName"`
	ContainerCreated   string            `json:"ContainerCreated"`
	ContainerEnv       []string          `json:"ContainerEnv"`
	ContainerLabels    map[string]string `json:"ContainerLabels"`
	LogPath            string            `json:"LogPath"`
	DaemonName         string            `json:"DaemonName"`
	Config             map[string]string `json:"Config"`
}

// StopLoggingRequest is sent by Docker when a container stops.
type StopLoggingRequest struct {
	File string `json:"File"`
}

// PluginResponse is the generic response to Docker RPC calls.
type PluginResponse struct {
	Err string `json:"Err"`
}

// CapabilitiesResponse reports the driver's capabilities.
type CapabilitiesResponse struct {
	Cap LogDriverCap `json:"Cap"`
}

// LogDriverCap describes what the driver supports.
type LogDriverCap struct {
	ReadLogs bool `json:"ReadLogs"`
}

// ReadLogsRequest is sent when `docker logs` is called.
type ReadLogsRequest struct {
	File   string         `json:"File"`
	Config ReadLogsConfig `json:"Config"`
}

// ReadLogsConfig describes the read parameters.
type ReadLogsConfig struct {
	Since  string `json:"Since"`
	Until  string `json:"Until"`
	Tail   int    `json:"Tail"`
	Follow bool   `json:"Follow"`
}

// -- HTTP Handlers --

// HandleStartLogging processes /LogDriver.StartLogging requests.
func (d *Driver) HandleStartLogging(w http.ResponseWriter, r *http.Request) {
	var req StartLoggingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, fmt.Sprintf("failed to decode request: %v", err))
		return
	}

	log.Printf("StartLogging: container=%s file=%s", req.Info.ContainerID[:12], req.File)

	batchMeta := ExtractBatchMetadata(req.Info.ContainerEnv)
	if batchMeta.TaskID != "" {
		log.Printf("  Batch context: job=%s task=%s pool=%s node=%s",
			batchMeta.JobID, batchMeta.TaskID, batchMeta.PoolID, batchMeta.NodeID)
	}

	info := &ContainerInfo{
		ContainerID:   req.Info.ContainerID,
		ContainerName: req.Info.ContainerName,
		BatchMeta:     batchMeta,
	}

	lp, err := NewLogPair(req.File, info, d.buffer)
	if err != nil {
		respondError(w, fmt.Sprintf("failed to open FIFO %s: %v", req.File, err))
		return
	}

	d.mu.Lock()
	d.logs[req.File] = lp
	d.mu.Unlock()

	respondOK(w)
}

// HandleStopLogging processes /LogDriver.StopLogging requests.
func (d *Driver) HandleStopLogging(w http.ResponseWriter, r *http.Request) {
	var req StopLoggingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, fmt.Sprintf("failed to decode request: %v", err))
		return
	}

	log.Printf("StopLogging: file=%s", req.File)

	d.mu.Lock()
	lp, ok := d.logs[req.File]
	if ok {
		delete(d.logs, req.File)
	}
	d.mu.Unlock()

	if ok {
		lp.Close()
	}

	respondOK(w)
}

// HandleCapabilities processes /LogDriver.Capabilities requests.
func (d *Driver) HandleCapabilities(w http.ResponseWriter, _ *http.Request) {
	resp := CapabilitiesResponse{
		Cap: LogDriverCap{ReadLogs: true},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleReadLogs processes /LogDriver.ReadLogs requests (supports `docker logs`).
func (d *Driver) HandleReadLogs(w http.ResponseWriter, r *http.Request) {
	var req ReadLogsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, fmt.Sprintf("failed to decode request: %v", err))
		return
	}

	d.mu.RLock()
	lp, ok := d.logs[req.File]
	d.mu.RUnlock()

	if !ok {
		respondError(w, fmt.Sprintf("no active log stream for %s", req.File))
		return
	}

	cached := lp.GetCachedLogs()

	// Apply tail limit
	if req.Config.Tail > 0 && req.Config.Tail < len(cached) {
		cached = cached[len(cached)-req.Config.Tail:]
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	for _, entry := range cached {
		enc.Encode(entry)
	}
}

// Close gracefully shuts down all active log consumers.
func (d *Driver) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for file, lp := range d.logs {
		lp.Close()
		delete(d.logs, file)
	}
}

// -- helpers --

func respondOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PluginResponse{Err: ""})
}

func respondError(w http.ResponseWriter, msg string) {
	log.Printf("ERROR: %s", msg)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PluginResponse{Err: msg})
}
