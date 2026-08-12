// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package azurelogsingestionexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/azurelogsingestionexporter"

import (
	"errors"
	"net/url"
	"strings"

	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

// Config defines configuration for the Azure Logs Ingestion exporter.
type Config struct {
	// Endpoint is the DCR logs ingestion endpoint URL.
	Endpoint string `mapstructure:"endpoint"`
	// RuleID is the DCR immutable ID (e.g., "dcr-xxx").
	RuleID string `mapstructure:"rule_id"`
	// StreamName is the DCR stream name (e.g., "Custom-CockroachDBLogs_CL").
	StreamName string `mapstructure:"stream_name"`
	// TenantID is the Azure Entra ID tenant ID.
	TenantID string `mapstructure:"tenant_id"`
	// ClientID is the Azure app registration client ID.
	ClientID string `mapstructure:"client_id"`
	// ClientSecret is the Azure app registration client secret.
	ClientSecret configopaque.String `mapstructure:"client_secret"`

	QueueSettings             configoptional.Optional[exporterhelper.QueueBatchConfig] `mapstructure:"sending_queue"`
	configretry.BackOffConfig `mapstructure:"retry_on_failure"`
	TimeoutSettings           exporterhelper.TimeoutConfig `mapstructure:",squash"`
}

func (cfg *Config) Validate() error {
	var errs []error

	if cfg.Endpoint == "" {
		errs = append(errs, errors.New("endpoint is required"))
	} else if u, err := url.Parse(cfg.Endpoint); err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, errors.New("endpoint must be a valid URL"))
	}

	if cfg.RuleID == "" {
		errs = append(errs, errors.New("rule_id is required"))
	} else if !strings.HasPrefix(cfg.RuleID, "dcr-") {
		errs = append(errs, errors.New("rule_id must start with 'dcr-'"))
	}

	if cfg.StreamName == "" {
		errs = append(errs, errors.New("stream_name is required"))
	}

	if cfg.TenantID == "" {
		errs = append(errs, errors.New("tenant_id is required"))
	}

	if cfg.ClientID == "" {
		errs = append(errs, errors.New("client_id is required"))
	}

	if cfg.ClientSecret == "" {
		errs = append(errs, errors.New("client_secret is required"))
	}

	return errors.Join(errs...)
}
