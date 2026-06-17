// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package azurelogsingestionexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/azurelogsingestionexporter"

import (
	"context"
	"fmt"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/azurelogsingestionexporter/internal/metadata"
)

// NewFactory returns a new factory for the Azure Logs Ingestion exporter.
func NewFactory() exporter.Factory {
	return exporter.NewFactory(
		metadata.Type,
		createDefaultConfig,
		exporter.WithLogs(createLogsExporter, metadata.LogsStability),
	)
}

func createDefaultConfig() component.Config {
	qs := configoptional.Default(exporterhelper.NewDefaultQueueConfig())

	return &Config{
		BackOffConfig:   configretry.NewDefaultBackOffConfig(),
		QueueSettings:   qs,
		TimeoutSettings: exporterhelper.NewDefaultTimeoutConfig(),
	}
}

func createLogsExporter(
	ctx context.Context,
	params exporter.Settings,
	cfg component.Config,
) (exporter.Logs, error) {
	exp, err := newLogsExporter(ctx, params, cfg.(*Config))
	if err != nil {
		return nil, fmt.Errorf("failed to create the Azure Logs Ingestion exporter: %w", err)
	}
	return exp, nil
}
