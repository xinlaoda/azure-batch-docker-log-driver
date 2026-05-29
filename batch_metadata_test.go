package main

import (
	"testing"
)

func TestExtractBatchMetadata_AllFields(t *testing.T) {
	env := []string{
		"AZ_BATCH_ACCOUNT_NAME=mybatchaccount",
		"AZ_BATCH_POOL_ID=mypool",
		"AZ_BATCH_NODE_ID=tvm-12345",
		"AZ_BATCH_JOB_ID=myjob",
		"AZ_BATCH_TASK_ID=mytask",
		"OTHER_VAR=ignored",
	}

	meta := ExtractBatchMetadata(env)

	if meta.AccountName != "mybatchaccount" {
		t.Errorf("AccountName = %q, want %q", meta.AccountName, "mybatchaccount")
	}
	if meta.PoolID != "mypool" {
		t.Errorf("PoolID = %q, want %q", meta.PoolID, "mypool")
	}
	if meta.NodeID != "tvm-12345" {
		t.Errorf("NodeID = %q, want %q", meta.NodeID, "tvm-12345")
	}
	if meta.JobID != "myjob" {
		t.Errorf("JobID = %q, want %q", meta.JobID, "myjob")
	}
	if meta.TaskID != "mytask" {
		t.Errorf("TaskID = %q, want %q", meta.TaskID, "mytask")
	}
}

func TestExtractBatchMetadata_Empty(t *testing.T) {
	meta := ExtractBatchMetadata(nil)

	if meta.AccountName != "" || meta.PoolID != "" || meta.NodeID != "" ||
		meta.JobID != "" || meta.TaskID != "" {
		t.Errorf("expected all empty fields, got %+v", meta)
	}
}

func TestExtractBatchMetadata_Partial(t *testing.T) {
	env := []string{
		"AZ_BATCH_JOB_ID=partjob",
		"AZ_BATCH_TASK_ID=parttask",
	}

	meta := ExtractBatchMetadata(env)

	if meta.JobID != "partjob" {
		t.Errorf("JobID = %q, want %q", meta.JobID, "partjob")
	}
	if meta.TaskID != "parttask" {
		t.Errorf("TaskID = %q, want %q", meta.TaskID, "parttask")
	}
	if meta.AccountName != "" {
		t.Errorf("AccountName should be empty, got %q", meta.AccountName)
	}
}

func TestExtractBatchMetadata_MalformedEntries(t *testing.T) {
	env := []string{
		"MALFORMED_NO_EQUALS",
		"AZ_BATCH_JOB_ID=validjob",
		"=emptykey",
	}

	meta := ExtractBatchMetadata(env)

	if meta.JobID != "validjob" {
		t.Errorf("JobID = %q, want %q", meta.JobID, "validjob")
	}
}

func TestExtractBatchMetadata_ValueWithEquals(t *testing.T) {
	env := []string{
		"AZ_BATCH_TASK_ID=task=with=equals",
	}

	meta := ExtractBatchMetadata(env)

	if meta.TaskID != "task=with=equals" {
		t.Errorf("TaskID = %q, want %q", meta.TaskID, "task=with=equals")
	}
}
