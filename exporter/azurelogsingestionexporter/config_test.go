// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package azurelogsingestionexporter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validConfig() *Config {
	return &Config{
		Endpoint:     "https://my-dce.eastus-1.ingest.monitor.azure.com",
		RuleID:       "dcr-abc123",
		StreamName:   "Custom-MyLogs_CL",
		TenantID:     "tenant-id",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	}
}

func TestConfigValidate(t *testing.T) {
	cfg := validConfig()
	assert.NoError(t, cfg.Validate())
}

func TestConfigValidateMissingEndpoint(t *testing.T) {
	cfg := validConfig()
	cfg.Endpoint = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint is required")
}

func TestConfigValidateInvalidEndpoint(t *testing.T) {
	cfg := validConfig()
	cfg.Endpoint = "not-a-url"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint must be a valid URL")
}

func TestConfigValidateMissingRuleID(t *testing.T) {
	cfg := validConfig()
	cfg.RuleID = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule_id is required")
}

func TestConfigValidateInvalidRuleIDPrefix(t *testing.T) {
	cfg := validConfig()
	cfg.RuleID = "invalid-prefix"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule_id must start with 'dcr-'")
}

func TestConfigValidateMissingStreamName(t *testing.T) {
	cfg := validConfig()
	cfg.StreamName = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream_name is required")
}

func TestConfigValidateMissingTenantID(t *testing.T) {
	cfg := validConfig()
	cfg.TenantID = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant_id is required")
}

func TestConfigValidateMissingClientID(t *testing.T) {
	cfg := validConfig()
	cfg.ClientID = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client_id is required")
}

func TestConfigValidateMissingClientSecret(t *testing.T) {
	cfg := validConfig()
	cfg.ClientSecret = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client_secret is required")
}

func TestConfigValidateMultipleErrors(t *testing.T) {
	cfg := &Config{}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint is required")
	assert.Contains(t, err.Error(), "rule_id is required")
	assert.Contains(t, err.Error(), "stream_name is required")
	assert.Contains(t, err.Error(), "tenant_id is required")
	assert.Contains(t, err.Error(), "client_id is required")
	assert.Contains(t, err.Error(), "client_secret is required")
}
