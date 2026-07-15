// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package publicegressextension // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/publicegressextension"

import (
	"context"
	"net"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/publicegressextension/internal/metadata"
)

// NewFactory creates a factory for the public egress extension.
func NewFactory() extension.Factory {
	return extension.NewFactory(
		metadata.Type,
		createDefaultConfig,
		createExtension,
		metadata.ExtensionStability,
	)
}

func createDefaultConfig() component.Config {
	return &Config{}
}

func createExtension(
	_ context.Context,
	_ extension.Settings,
	_ component.Config,
) (extension.Extension, error) {
	return newPublicEgressExtension(net.DefaultResolver, &net.Dialer{
		Timeout:   defaultDialTimeout,
		KeepAlive: defaultKeepAlive,
	}), nil
}
