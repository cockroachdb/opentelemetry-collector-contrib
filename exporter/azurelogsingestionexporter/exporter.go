// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package azurelogsingestionexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/azurelogsingestionexporter"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/ingestion/azlogs"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"
)

const maxPayloadBytes = 900 * 1024 // 900KB, under the 1MB API limit

type chunkedRecords struct {
	chunks  [][]byte
	dropped int
}

type azureLogsIngestionExporter struct {
	config *Config
	client *azlogs.Client
	logger *zap.Logger
}

func initExporter(cfg *Config, settings exporter.Settings) (*azureLogsIngestionExporter, error) {
	cred, err := azidentity.NewClientSecretCredential(cfg.TenantID, cfg.ClientID, string(cfg.ClientSecret), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure credential: %w", err)
	}

	client, err := azlogs.NewClient(cfg.Endpoint, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure Logs Ingestion client: %w", err)
	}

	settings.Logger.Info("Azure Logs Ingestion Exporter configured",
		zap.String("endpoint", cfg.Endpoint),
		zap.String("rule_id", cfg.RuleID),
		zap.String("stream_name", cfg.StreamName),
	)

	return &azureLogsIngestionExporter{
		config: cfg,
		client: client,
		logger: settings.Logger,
	}, nil
}

func newLogsExporter(
	ctx context.Context,
	params exporter.Settings,
	cfg *Config,
) (exporter.Logs, error) {
	exp, err := initExporter(cfg, params)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Azure Logs Ingestion exporter: %w", err)
	}

	return exporterhelper.NewLogs(
		ctx,
		params,
		cfg,
		exp.pushLogsData,
		exporterhelper.WithTimeout(cfg.TimeoutSettings),
		exporterhelper.WithRetry(cfg.BackOffConfig),
		exporterhelper.WithQueue(cfg.QueueSettings),
	)
}

func (e *azureLogsIngestionExporter) pushLogsData(ctx context.Context, logs plog.Logs) error {
	var records []map[string]any

	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		resourceLogs := logs.ResourceLogs().At(i)
		resource := resourceLogs.Resource()
		for j := 0; j < resourceLogs.ScopeLogs().Len(); j++ {
			scopeLogs := resourceLogs.ScopeLogs().At(j)
			for k := 0; k < scopeLogs.LogRecords().Len(); k++ {
				record := scopeLogs.LogRecords().At(k)
				records = append(records, serializeLogRecord(record, resource))
			}
		}
	}

	if len(records) == 0 {
		return nil
	}

	chunked, err := chunkRecords(records)
	if err != nil {
		return consumererror.NewLogs(fmt.Errorf("failed to serialize log records: %w", err), logs)
	}

	if chunked.dropped > 0 {
		e.logger.Warn("Dropped oversized Azure Logs Ingestion records",
			zap.Int("dropped_records", chunked.dropped),
			zap.Int("max_payload_bytes", maxPayloadBytes),
		)
	}

	if len(chunked.chunks) == 0 {
		return consumererror.NewLogs(consumererror.NewPermanent(
			fmt.Errorf("all log records exceeded Azure Logs Ingestion payload limit of %d bytes", maxPayloadBytes),
		), logs)
	}

	for _, chunk := range chunked.chunks {
		if _, err := e.client.Upload(ctx, e.config.RuleID, e.config.StreamName, chunk, nil); err != nil {
			return consumererror.NewLogs(classifyUploadError(err), logs)
		}
	}

	return nil
}

func chunkRecords(records []map[string]any) (chunkedRecords, error) {
	var chunks [][]byte
	var current []map[string]any
	var dropped int
	currentSize := 2 // account for JSON array brackets "[]"

	for _, record := range records {
		recordBytes, err := json.Marshal(record)
		if err != nil {
			return chunkedRecords{}, err
		}

		if len(recordBytes)+2 > maxPayloadBytes {
			dropped++
			continue
		}

		recordSize := len(recordBytes)
		if len(current) > 0 {
			recordSize++ // comma separator
		}

		if currentSize+recordSize > maxPayloadBytes && len(current) > 0 {
			chunk, err := json.Marshal(current)
			if err != nil {
				return chunkedRecords{}, err
			}
			chunks = append(chunks, chunk)
			current = nil
			currentSize = 2
		}

		current = append(current, record)
		currentSize += len(recordBytes)
		if len(current) > 1 {
			currentSize++ // comma separator
		}
	}

	if len(current) > 0 {
		chunk, err := json.Marshal(current)
		if err != nil {
			return chunkedRecords{}, err
		}
		chunks = append(chunks, chunk)
	}

	return chunkedRecords{
		chunks:  chunks,
		dropped: dropped,
	}, nil
}

func classifyUploadError(err error) error {
	uploadErr := fmt.Errorf("failed to upload logs: %w", err)

	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		return uploadErr
	}

	switch {
	case responseErr.StatusCode == http.StatusTooManyRequests:
		if delay := retryAfterDelay(responseErr.RawResponse); delay > 0 {
			return exporterhelper.NewThrottleRetry(uploadErr, delay)
		}
		return uploadErr
	case responseErr.StatusCode == http.StatusRequestTimeout || responseErr.StatusCode >= http.StatusInternalServerError:
		return uploadErr
	case responseErr.StatusCode >= http.StatusBadRequest && responseErr.StatusCode < http.StatusInternalServerError:
		return consumererror.NewPermanent(uploadErr)
	default:
		return uploadErr
	}
}

func retryAfterDelay(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}

	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(retryAfter); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}

	retryAt, err := http.ParseTime(retryAfter)
	if err != nil {
		return 0
	}
	return time.Until(retryAt)
}
