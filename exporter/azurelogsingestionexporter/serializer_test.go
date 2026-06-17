// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package azurelogsingestionexporter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func TestSerializeLogRecordMapBody(t *testing.T) {
	record := plog.NewLogRecord()
	record.SetTimestamp(pcommon.NewTimestampFromTime(time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)))

	body := record.Body().SetEmptyMap()
	body.PutStr("severity", "ERROR")
	body.PutStr("channel", "SQL_EXEC")
	body.PutStr("message", "something went wrong")
	body.PutInt("goroutine", 1022)
	body.PutStr("file", "kvprober.go")
	body.PutInt("line", 488)
	body.PutInt("node_id", 3)
	body.PutStr("cloud_cluster_id", "cluster-123")
	body.PutStr("cloud_cluster_name", "my-cluster")

	resource := pcommon.NewResource()
	result := serializeLogRecord(record, resource)

	assert.Equal(t, "ERROR", result["severity"])
	assert.Equal(t, "SQL_EXEC", result["channel"])
	assert.Equal(t, "something went wrong", result["message"])
	assert.Equal(t, int64(1022), result["goroutine"])
	assert.Equal(t, "kvprober.go", result["file"])
	assert.Equal(t, int64(488), result["line"])
	assert.Equal(t, int64(3), result["node_id"])
	assert.Equal(t, "cluster-123", result["cloud_cluster_id"])
	assert.Equal(t, "my-cluster", result["cloud_cluster_name"])

	ts, ok := result["TimeGenerated"].(string)
	require.True(t, ok)
	assert.Contains(t, ts, "2026-05-27T12:00:00")
}

func TestSerializeLogRecordStringBody(t *testing.T) {
	record := plog.NewLogRecord()
	record.SetTimestamp(pcommon.NewTimestampFromTime(time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)))
	record.Body().SetStr("raw log line that was not parsed")

	resource := pcommon.NewResource()
	result := serializeLogRecord(record, resource)

	assert.Equal(t, "raw log line that was not parsed", result["message"])

	ts, ok := result["TimeGenerated"].(string)
	require.True(t, ok)
	assert.Contains(t, ts, "2026-05-27T12:00:00")
}

func TestSerializeLogRecordNestedFields(t *testing.T) {
	record := plog.NewLogRecord()
	record.SetTimestamp(pcommon.NewTimestampFromTime(time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)))

	body := record.Body().SetEmptyMap()
	body.PutStr("severity", "INFO")
	body.PutStr("message", "runtime stats")
	tags := body.PutEmptyMap("tags")
	tags.PutStr("n", "1")
	tags.PutStr("kvprober", "")

	resource := pcommon.NewResource()
	result := serializeLogRecord(record, resource)

	tagsResult, ok := result["tags"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "1", tagsResult["n"])
	assert.Equal(t, "", tagsResult["kvprober"])
}

func TestSerializeLogRecordZeroTimestamp(t *testing.T) {
	record := plog.NewLogRecord()
	record.Body().SetStr("no timestamp")

	resource := pcommon.NewResource()
	result := serializeLogRecord(record, resource)

	ts, ok := result["TimeGenerated"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, ts)

	parsed, err := time.Parse(time.RFC3339Nano, ts)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().UTC(), parsed, 5*time.Second)
}
