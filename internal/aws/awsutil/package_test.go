// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awsutil

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	tempDir, err := os.MkdirTemp("", "awsutil-test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tempDir)

	configFile := filepath.Join(tempDir, "config")
	credentialsFile := filepath.Join(tempDir, "credentials")
	if err = os.WriteFile(configFile, nil, 0o600); err != nil {
		panic(err)
	}
	if err = os.WriteFile(credentialsFile, nil, 0o600); err != nil {
		panic(err)
	}
	if err = os.Setenv("AWS_CONFIG_FILE", configFile); err != nil {
		panic(err)
	}
	if err = os.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsFile); err != nil {
		panic(err)
	}

	goleak.VerifyTestMain(m)
}
