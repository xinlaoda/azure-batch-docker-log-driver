package main

import (
	"context"
	"log"
	"sync"
	"time"
)

// LogBuffer accumulates log entries and flushes them in batches to Azure Monitor.
type LogBuffer struct {
	mu       sync.Mutex
	entries  []LogEntry
	maxSize  int
	interval time.Duration
	client   *AzureClient
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	flushCh  chan struct{}
}

// NewLogBuffer creates a buffer that flushes when maxSize entries accumulate
// or after interval elapses, whichever comes first.
func NewLogBuffer(client *AzureClient, maxSize int, interval time.Duration) *LogBuffer {
	ctx, cancel := context.WithCancel(context.Background())
	b := &LogBuffer{
		entries:  make([]LogEntry, 0, maxSize),
		maxSize:  maxSize,
		interval: interval,
		client:   client,
		ctx:      ctx,
		cancel:   cancel,
		flushCh:  make(chan struct{}, 1),
	}
	b.wg.Add(1)
	go b.flushLoop()
	return b
}

// Add appends a log entry to the buffer and triggers a flush if the buffer is full.
func (b *LogBuffer) Add(entry LogEntry) {
	b.mu.Lock()
	b.entries = append(b.entries, entry)
	shouldFlush := len(b.entries) >= b.maxSize
	b.mu.Unlock()

	if shouldFlush {
		select {
		case b.flushCh <- struct{}{}:
		default:
		}
	}
}

// flushLoop periodically flushes or responds to size-triggered flushes.
func (b *LogBuffer) flushLoop() {
	defer b.wg.Done()
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			b.flush()
			return
		case <-ticker.C:
			b.flush()
		case <-b.flushCh:
			b.flush()
		}
	}
}

// flush sends all buffered entries to Azure Monitor.
func (b *LogBuffer) flush() {
	b.mu.Lock()
	if len(b.entries) == 0 {
		b.mu.Unlock()
		return
	}
	batch := b.entries
	b.entries = make([]LogEntry, 0, b.maxSize)
	b.mu.Unlock()

	debugLog("flushing %d log entries to Azure Monitor", len(batch))
	if err := b.client.Upload(b.ctx, batch); err != nil {
		log.Printf("failed to flush %d log entries: %v", len(batch), err)
		// Re-enqueue on failure (with size cap to prevent OOM)
		b.mu.Lock()
		if len(b.entries)+len(batch) <= b.maxSize*10 {
			b.entries = append(batch, b.entries...)
		} else {
			log.Printf("dropping %d log entries (buffer overflow)", len(batch))
		}
		b.mu.Unlock()
	}
}

// Close flushes remaining entries and stops the background goroutine.
func (b *LogBuffer) Close() {
	b.cancel()
	b.wg.Wait()
}
