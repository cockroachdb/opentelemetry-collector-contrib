// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package azurelogsingestionexporter

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"
)

func TestChunkRecordsSingleChunk(t *testing.T) {
	records := []map[string]any{
		{"TimeGenerated": "2026-05-27T12:00:00Z", "Message": "test1"},
		{"TimeGenerated": "2026-05-27T12:00:01Z", "Message": "test2"},
	}

	chunked, err := chunkRecords(records)
	require.NoError(t, err)
	assert.Zero(t, chunked.dropped)
	assert.Len(t, chunked.chunks, 1)

	var parsed []map[string]any
	require.NoError(t, json.Unmarshal(chunked.chunks[0], &parsed))
	assert.Len(t, parsed, 2)
	assert.Equal(t, "test1", parsed[0]["Message"])
	assert.Equal(t, "test2", parsed[1]["Message"])
}

func TestChunkRecordsMultipleChunks(t *testing.T) {
	// Create records large enough to exceed maxPayloadBytes
	var records []map[string]any
	largeMessage := strings.Repeat("x", 100*1024) // 100KB per record
	for i := 0; i < 15; i++ {
		records = append(records, map[string]any{
			"TimeGenerated": "2026-05-27T12:00:00Z",
			"Message":       largeMessage,
		})
	}

	chunked, err := chunkRecords(records)
	require.NoError(t, err)
	assert.Zero(t, chunked.dropped)
	assert.Greater(t, len(chunked.chunks), 1)

	totalRecords := 0
	for _, chunk := range chunked.chunks {
		assert.LessOrEqual(t, len(chunk), maxPayloadBytes+1024) // allow some overhead for JSON encoding
		var parsed []map[string]any
		require.NoError(t, json.Unmarshal(chunk, &parsed))
		totalRecords += len(parsed)
	}
	assert.Equal(t, 15, totalRecords)
}

func TestChunkRecordsEmpty(t *testing.T) {
	chunked, err := chunkRecords(nil)
	require.NoError(t, err)
	assert.Empty(t, chunked.chunks)
	assert.Zero(t, chunked.dropped)
}

func TestChunkRecordsSingleLargeRecord(t *testing.T) {
	records := []map[string]any{
		{"TimeGenerated": "2026-05-27T12:00:00Z", "Message": strings.Repeat("x", 500*1024)},
	}

	chunked, err := chunkRecords(records)
	require.NoError(t, err)
	assert.Zero(t, chunked.dropped)
	assert.Len(t, chunked.chunks, 1)
}

func TestChunkRecordsDropsOversizedRecord(t *testing.T) {
	records := []map[string]any{
		{"TimeGenerated": "2026-05-27T12:00:00Z", "Message": strings.Repeat("x", maxPayloadBytes)},
	}

	chunked, err := chunkRecords(records)
	require.NoError(t, err)
	assert.Empty(t, chunked.chunks)
	assert.Equal(t, 1, chunked.dropped)
}

func TestChunkRecordsDropsOnlyOversizedRecords(t *testing.T) {
	records := []map[string]any{
		{"TimeGenerated": "2026-05-27T12:00:00Z", "Message": "small"},
		{"TimeGenerated": "2026-05-27T12:00:01Z", "Message": strings.Repeat("x", maxPayloadBytes)},
		{"TimeGenerated": "2026-05-27T12:00:02Z", "Message": "small2"},
	}

	chunked, err := chunkRecords(records)
	require.NoError(t, err)
	assert.Equal(t, 1, chunked.dropped)
	assert.Len(t, chunked.chunks, 1)

	var parsed []map[string]any
	require.NoError(t, json.Unmarshal(chunked.chunks[0], &parsed))
	assert.Len(t, parsed, 2)
	assert.Equal(t, "small", parsed[0]["Message"])
	assert.Equal(t, "small2", parsed[1]["Message"])
}

func TestPushLogsDataAllOversizedRecordsPermanent(t *testing.T) {
	logs := plog.NewLogs()
	record := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	record.SetTimestamp(pcommon.NewTimestampFromTime(time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)))
	record.Body().SetStr(strings.Repeat("x", maxPayloadBytes))

	exp := &azureLogsIngestionExporter{logger: zap.NewNop()}
	err := exp.pushLogsData(t.Context(), logs)
	require.Error(t, err)
	assert.True(t, consumererror.IsPermanent(err))
}

func TestClassifyUploadErrorPermanentStatus(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusRequestEntityTooLarge,
		http.StatusUnsupportedMediaType,
		http.StatusUnprocessableEntity,
	} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			err := consumererror.NewLogs(classifyUploadError(responseError(statusCode, "")), plog.NewLogs())
			assert.True(t, consumererror.IsPermanent(err))
		})
	}
}

func TestClassifyUploadErrorRetryableStatus(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			err := classifyUploadError(responseError(statusCode, ""))
			assert.False(t, consumererror.IsPermanent(err))
		})
	}
}

func TestClassifyUploadErrorThrottleRetryAfter(t *testing.T) {
	err := classifyUploadError(responseError(http.StatusTooManyRequests, "2"))
	assert.False(t, consumererror.IsPermanent(err))
	assert.Contains(t, err.Error(), "Throttle (2s)")
}

func TestClassifyUploadErrorNonAzureErrorRetryable(t *testing.T) {
	err := classifyUploadError(errors.New("connection reset by peer"))
	assert.False(t, consumererror.IsPermanent(err))
	assert.Contains(t, err.Error(), "failed to upload logs")
}

func TestRetryAfterDelayHTTPDate(t *testing.T) {
	retryAt := time.Now().Add(2 * time.Second).UTC()
	delay := retryAfterDelay(&http.Response{
		Header: http.Header{"Retry-After": []string{retryAt.Format(http.TimeFormat)}},
	})
	assert.Greater(t, delay, time.Duration(0))
}

func responseError(statusCode int, retryAfter string) error {
	header := http.Header{}
	if retryAfter != "" {
		header.Set("Retry-After", retryAfter)
	}
	return &azcore.ResponseError{
		StatusCode: statusCode,
		RawResponse: &http.Response{
			StatusCode: statusCode,
			Status:     http.StatusText(statusCode),
			Header:     header,
		},
	}
}
