// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package azurelogsingestionexporter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/exporter/exportertest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/azurelogsingestionexporter/internal/metadata"
)

func TestNewFactory(t *testing.T) {
	f := NewFactory()
	require.NotNil(t, f)
	assert.Equal(t, "azurelogsingestion", f.Type().String())
}

func TestCreateDefaultConfig(t *testing.T) {
	f := NewFactory()
	cfg := f.CreateDefaultConfig()
	require.NotNil(t, cfg)
	assert.IsType(t, &Config{}, cfg)
}

func TestCreateLogsExporterInvalidConfig(t *testing.T) {
	_, err := createLogsExporter(
		t.Context(),
		exportertest.NewNopSettings(metadata.Type),
		createDefaultConfig(),
	)
	require.Error(t, err)
}

func TestCreateLogsExporterValidConfig(t *testing.T) {
	cfg := validConfig()
	exp, err := createLogsExporter(
		t.Context(),
		exportertest.NewNopSettings(metadata.Type),
		cfg,
	)
	// Azure SDK accepts any credential values at creation time; validation
	// only happens on first token request. So the exporter is created
	// successfully with fake credentials.
	require.NoError(t, err)
	require.NotNil(t, exp)
}
