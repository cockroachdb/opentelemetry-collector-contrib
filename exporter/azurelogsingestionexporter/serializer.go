// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package azurelogsingestionexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/azurelogsingestionexporter"

import (
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func serializeLogRecord(record plog.LogRecord, resource pcommon.Resource) map[string]any {
	var entry map[string]any

	raw := record.Body().AsRaw()
	if m, ok := raw.(map[string]any); ok {
		entry = m
	} else {
		entry = map[string]any{"message": record.Body().AsString()}
	}

	entry["TimeGenerated"] = formatTimestamp(record.Timestamp())
	return entry
}

func formatTimestamp(ts pcommon.Timestamp) string {
	if ts == 0 {
		return time.Now().UTC().Format(time.RFC3339Nano)
	}
	return ts.AsTime().UTC().Format(time.RFC3339Nano)
}
