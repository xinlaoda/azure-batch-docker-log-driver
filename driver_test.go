package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleCapabilities(t *testing.T) {
	cfg := DefaultPluginConfig()
	cfg.DCEEndpoint = "https://test"
	cfg.DCRImmutableID = "dcr-test"
	cfg.DCRStreamName = "Custom-Test_CL"

	driver := NewDriver(cfg, nil)

	req := httptest.NewRequest(http.MethodPost, "/LogDriver.Capabilities", nil)
	w := httptest.NewRecorder()

	driver.HandleCapabilities(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp CapabilitiesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Cap.ReadLogs {
		t.Error("expected ReadLogs capability to be true")
	}
}

func TestHandleStopLogging_NotFound(t *testing.T) {
	cfg := DefaultPluginConfig()
	cfg.DCEEndpoint = "https://test"
	cfg.DCRImmutableID = "dcr-test"
	cfg.DCRStreamName = "Custom-Test_CL"

	driver := NewDriver(cfg, nil)

	body := StopLoggingRequest{File: "/nonexistent/fifo"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/LogDriver.StopLogging", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	driver.HandleStopLogging(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp PluginResponse
	json.NewDecoder(w.Body).Decode(&resp)

	// StopLogging for unknown file should succeed (idempotent)
	if resp.Err != "" {
		t.Errorf("expected no error, got %q", resp.Err)
	}
}

func TestHandleStartLogging_InvalidJSON(t *testing.T) {
	cfg := DefaultPluginConfig()
	cfg.DCEEndpoint = "https://test"
	cfg.DCRImmutableID = "dcr-test"
	cfg.DCRStreamName = "Custom-Test_CL"

	driver := NewDriver(cfg, nil)

	req := httptest.NewRequest(http.MethodPost, "/LogDriver.StartLogging",
		bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	driver.HandleStartLogging(w, req)

	var resp PluginResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Err == "" {
		t.Error("expected error for invalid JSON")
	}
}

func TestHandleReadLogs_NotFound(t *testing.T) {
	cfg := DefaultPluginConfig()
	cfg.DCEEndpoint = "https://test"
	cfg.DCRImmutableID = "dcr-test"
	cfg.DCRStreamName = "Custom-Test_CL"

	driver := NewDriver(cfg, nil)

	body := ReadLogsRequest{File: "/nonexistent/fifo"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/LogDriver.ReadLogs", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	driver.HandleReadLogs(w, req)

	var resp PluginResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Err == "" {
		t.Error("expected error for nonexistent log stream")
	}
}
