// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package publicegressextension // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/publicegressextension"

// Config configures the public egress extension. The extension intentionally
// has no policy knobs: when attached to an HTTP client it always requires HTTPS,
// disables proxy use, rejects redirects, and permits only public IP addresses.
type Config struct {
	// prevent unkeyed literal initialization
	_ struct{}
}
