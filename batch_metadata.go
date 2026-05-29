package main

import "strings"

// BatchMetadata holds Azure Batch context extracted from container environment variables.
type BatchMetadata struct {
	AccountName   string `json:"BatchAccountName,omitempty"`
	PoolID        string `json:"PoolId,omitempty"`
	NodeID        string `json:"NodeId,omitempty"`
	JobID         string `json:"JobId,omitempty"`
	TaskID        string `json:"TaskId,omitempty"`
}

// envMapping maps AZ_BATCH_* environment variable names to BatchMetadata fields.
var envMapping = map[string]func(*BatchMetadata, string){
	"AZ_BATCH_ACCOUNT_NAME": func(m *BatchMetadata, v string) { m.AccountName = v },
	"AZ_BATCH_POOL_ID":      func(m *BatchMetadata, v string) { m.PoolID = v },
	"AZ_BATCH_NODE_ID":      func(m *BatchMetadata, v string) { m.NodeID = v },
	"AZ_BATCH_JOB_ID":       func(m *BatchMetadata, v string) { m.JobID = v },
	"AZ_BATCH_TASK_ID":      func(m *BatchMetadata, v string) { m.TaskID = v },
}

// ExtractBatchMetadata parses AZ_BATCH_* environment variables from the container
// environment list (format: "KEY=VALUE").
func ExtractBatchMetadata(containerEnv []string) *BatchMetadata {
	meta := &BatchMetadata{}
	for _, env := range containerEnv {
		key, value, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}
		if setter, found := envMapping[key]; found {
			setter(meta, value)
		}
	}
	return meta
}
