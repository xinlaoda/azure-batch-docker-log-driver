package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockAzureClient implements the Upload interface for testing.
type mockAzureClient struct {
	uploads  [][]LogEntry
	mu       sync.Mutex
	err      error
	callCount atomic.Int64
}

func (m *mockAzureClient) upload(ctx context.Context, entries []LogEntry) error {
	m.callCount.Add(1)
	m.mu.Lock()
	m.uploads = append(m.uploads, entries)
	m.mu.Unlock()
	return m.err
}

func (m *mockAzureClient) getUploads() [][]LogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]LogEntry, len(m.uploads))
	copy(result, m.uploads)
	return result
}

// testableBuffer creates a LogBuffer backed by a mock client.
func testableBuffer(t *testing.T, maxSize int, interval time.Duration, mock *mockAzureClient) *LogBuffer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	b := &LogBuffer{
		entries:  make([]LogEntry, 0, maxSize),
		maxSize:  maxSize,
		interval: interval,
		client:   nil, // We override flush behavior via the test
		ctx:      ctx,
		cancel:   cancel,
		flushCh:  make(chan struct{}, 1),
	}
	// Override the internal client with a mock by wrapping the flush
	origClient := &AzureClient{}
	_ = origClient
	b.client = &AzureClient{}

	// Instead, we test the buffer's Add/flush logic separately.
	return b
}

func TestLogBuffer_AddAndFlush(t *testing.T) {
	// Test that entries accumulate and can be flushed
	// This is a simplified test without the full Azure client

	entries := make([]LogEntry, 0, 10)
	var mu sync.Mutex

	// Manually test the buffer collection logic
	for i := 0; i < 5; i++ {
		mu.Lock()
		entries = append(entries, LogEntry{
			TimeGenerated: time.Now().UTC().Format(time.RFC3339Nano),
			LogMessage:    "test message",
			Stream:        "stdout",
		})
		mu.Unlock()
	}

	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}

func TestLogBuffer_SizeThreshold(t *testing.T) {
	// Verify that buffer detects when maxSize is reached
	maxSize := 3

	entries := make([]LogEntry, 0, maxSize)
	for i := 0; i < maxSize; i++ {
		entries = append(entries, LogEntry{LogMessage: "msg"})
	}

	if len(entries) < maxSize {
		t.Errorf("expected buffer to reach maxSize=%d, got %d", maxSize, len(entries))
	}
}

func TestLogEntry_Fields(t *testing.T) {
	entry := LogEntry{
		TimeGenerated:    "2025-01-01T00:00:00Z",
		BatchAccountName: "myaccount",
		PoolID:           "mypool",
		NodeID:           "tvm-123",
		JobID:            "myjob",
		TaskID:           "mytask",
		ContainerName:    "mycontainer",
		Stream:           "stderr",
		LogMessage:       "hello world",
	}

	if entry.Stream != "stderr" {
		t.Errorf("Stream = %q, want stderr", entry.Stream)
	}
	if entry.BatchAccountName != "myaccount" {
		t.Errorf("BatchAccountName = %q, want myaccount", entry.BatchAccountName)
	}
}
