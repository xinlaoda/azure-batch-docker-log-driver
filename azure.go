package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/ingestion/azlogs"
)

// LogEntry is the payload sent to Azure Monitor Log Analytics via the Ingestion API.
type LogEntry struct {
	TimeGenerated    string `json:"TimeGenerated"`
	BatchAccountName string `json:"BatchAccountName,omitempty"`
	PoolID           string `json:"PoolId,omitempty"`
	NodeID           string `json:"NodeId,omitempty"`
	JobID            string `json:"JobId,omitempty"`
	TaskID           string `json:"TaskId,omitempty"`
	ContainerName    string `json:"ContainerName,omitempty"`
	Stream           string `json:"Stream"`
	LogMessage       string `json:"LogMessage"`
}

// AzureClient wraps the Azure Monitor Logs Ingestion client.
type AzureClient struct {
	client         *azlogs.Client
	dcrImmutableID string
	streamName     string
}

// NewAzureClient creates a new ingestion client using Managed Identity.
func NewAzureClient(cfg *PluginConfig) (*AzureClient, error) {
	var cred *azidentity.ManagedIdentityCredential
	var err error

	if cfg.ManagedIdentityClientID != "" {
		opts := &azidentity.ManagedIdentityCredentialOptions{
			ID: azidentity.ClientID(cfg.ManagedIdentityClientID),
		}
		cred, err = azidentity.NewManagedIdentityCredential(opts)
	} else {
		cred, err = azidentity.NewManagedIdentityCredential(nil)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create managed identity credential: %w", err)
	}

	client, err := azlogs.NewClient(cfg.DCEEndpoint, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create azlogs client: %w", err)
	}

	return &AzureClient{
		client:         client,
		dcrImmutableID: cfg.DCRImmutableID,
		streamName:     cfg.DCRStreamName,
	}, nil
}

// Upload sends a batch of log entries to Azure Monitor with retry logic.
func (ac *AzureClient) Upload(ctx context.Context, entries []LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Convert to JSON bytes for the SDK
	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to marshal log entries: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		debugLog("uploading %d entries (attempt %d)", len(entries), attempt+1)
		_, err := ac.client.Upload(ctx, ac.dcrImmutableID, ac.streamName, data, nil)
		if err == nil {
			debugLog("upload succeeded: %d entries", len(entries))
			return nil
		}
		lastErr = err
		log.Printf("azure upload attempt %d failed: %v", attempt+1, err)

		// Exponential backoff: 1s, 2s, 4s
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * time.Second):
		}
	}
	return fmt.Errorf("azure upload failed after 3 attempts: %w", lastErr)
}
