package main

import (
	"context"
	"encoding/binary"
	"io"
	"log"
	"sync"
	"time"

	"github.com/containerd/fifo"
	"github.com/docker/docker/api/types/plugins/logdriver"
	"github.com/gogo/protobuf/proto"
)

// LogPair manages reading logs from a single container's FIFO stream.
type LogPair struct {
	stream    io.ReadCloser
	info      *ContainerInfo
	buf       *LogBuffer
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once

	// For ReadLogs support
	mu       sync.Mutex
	logCache []LogEntry
	maxCache int
}

// ContainerInfo holds metadata about the container being logged.
type ContainerInfo struct {
	ContainerID   string
	ContainerName string
	BatchMeta     *BatchMetadata
}

// NewLogPair creates a LogPair that reads from the specified FIFO file.
func NewLogPair(fifoPath string, info *ContainerInfo, buf *LogBuffer) (*LogPair, error) {
	ctx, cancel := context.WithCancel(context.Background())

	f, err := fifo.OpenFifo(ctx, fifoPath, 0, 0)
	if err != nil {
		cancel()
		return nil, err
	}

	lp := &LogPair{
		stream:   f,
		info:     info,
		buf:      buf,
		ctx:      ctx,
		cancel:   cancel,
		maxCache: 1000,
	}

	lp.wg.Add(1)
	go lp.consumeLog()
	return lp, nil
}

// consumeLog reads protobuf-encoded log entries from the FIFO and sends them to the buffer.
func (lp *LogPair) consumeLog() {
	defer lp.wg.Done()
	defer lp.closeStream()

	for {
		if lp.ctx.Err() != nil {
			return
		}

		entry, err := decodeLogEntry(lp.stream)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF || lp.ctx.Err() != nil {
				return
			}
			// Stream closed by Close() also produces read errors; check context
			if lp.ctx.Err() != nil {
				return
			}
			log.Printf("error decoding log entry for %s: %v", lp.info.ContainerID, err)
			return
		}

		streamName := "stdout"
		if entry.Source == "stderr" {
			streamName = "stderr"
		}

		ts := time.Unix(entry.TimeNano/int64(time.Second), entry.TimeNano%int64(time.Second))

		logEntry := LogEntry{
			TimeGenerated: ts.UTC().Format(time.RFC3339Nano),
			ContainerName: lp.info.ContainerName,
			Stream:        streamName,
			LogMessage:    string(entry.Line),
		}

		if lp.info.BatchMeta != nil {
			logEntry.BatchAccountName = lp.info.BatchMeta.AccountName
			logEntry.PoolID = lp.info.BatchMeta.PoolID
			logEntry.NodeID = lp.info.BatchMeta.NodeID
			logEntry.JobID = lp.info.BatchMeta.JobID
			logEntry.TaskID = lp.info.BatchMeta.TaskID
		}

		lp.buf.Add(logEntry)
		debugLog("container=%s stream=%s msg=%s", lp.info.ContainerID[:12], streamName, string(entry.Line))

		// Cache for ReadLogs
		lp.mu.Lock()
		if len(lp.logCache) >= lp.maxCache {
			lp.logCache = lp.logCache[1:]
		}
		lp.logCache = append(lp.logCache, logEntry)
		lp.mu.Unlock()
	}
}

// decodeLogEntry reads a single protobuf-encoded log entry from the stream.
// Docker uses a 4-byte big-endian length prefix followed by the protobuf message.
func decodeLogEntry(r io.Reader) (*logdriver.LogEntry, error) {
	var sizeBuf [4]byte
	if _, err := io.ReadFull(r, sizeBuf[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(sizeBuf[:])

	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	entry := &logdriver.LogEntry{}
	if err := proto.Unmarshal(data, entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// GetCachedLogs returns a copy of the cached log entries for ReadLogs support.
func (lp *LogPair) GetCachedLogs() []LogEntry {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	result := make([]LogEntry, len(lp.logCache))
	copy(result, lp.logCache)
	return result
}

// closeStream safely closes the FIFO stream exactly once.
func (lp *LogPair) closeStream() {
	lp.closeOnce.Do(func() {
		lp.stream.Close()
	})
}

// Close stops reading from the FIFO and waits for the consumer goroutine to finish.
// It closes the stream first to unblock any pending io.ReadFull calls.
func (lp *LogPair) Close() {
	lp.cancel()
	lp.closeStream()
	lp.wg.Wait()
}
