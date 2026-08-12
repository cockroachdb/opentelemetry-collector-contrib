// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awss3exporter

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/config/configcompression"
	"go.uber.org/zap"
)

func TestNewUploadManager(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		conf   *Config
		errVal string
	}{
		{
			name: "valid configuration",
			conf: &Config{
				S3Uploader: S3UploaderConfig{
					Region:              "local",
					S3Bucket:            "my-awesome-bucket",
					S3Prefix:            "opentelemetry",
					S3PartitionFormat:   "year=%Y/month=%m/day=%d/hour=%H",
					S3PartitionTimezone: "Europe/London",
					FilePrefix:          "ingested-data-",
					Endpoint:            "localhost",
					RoleArn:             "arn:aws:iam::123456789012:my-awesome-user",
					S3ForcePathStyle:    true,
					DisableSSL:          true,
					Compression:         configcompression.TypeGzip,
				},
			},
			errVal: "",
		},
		{
			name: "invalid timezone configuration",
			conf: &Config{
				S3Uploader: S3UploaderConfig{
					Region:              "local",
					S3Bucket:            "my-awesome-bucket",
					S3Prefix:            "opentelemetry",
					S3PartitionFormat:   "year=%Y/month=%m/day=%d/hour=%H",
					S3PartitionTimezone: "non-existing timezone",
					FilePrefix:          "ingested-data-",
					Endpoint:            "localhost",
					RoleArn:             "arn:aws:iam::123456789012:my-awesome-user",
					S3ForcePathStyle:    true,
					DisableSSL:          true,
					Compression:         configcompression.TypeGzip,
				},
			},
			errVal: "invalid S3 partition timezone: unknown time zone non-existing timezone",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sm, err := newUploadManager(
				t.Context(),
				tc.conf,
				zap.NewNop(),
				"metrics",
				"otlp",
				false,
			)

			if tc.errVal != "" {
				assert.Nil(t, sm, "Must not have a valid s3 upload manager")
				assert.EqualError(t, err, tc.errVal, "Must match the expected error")
			} else {
				assert.NotNil(t, sm, "Must have a valid manager")
				assert.NoError(t, err, "Must not error when creating client")
			}
		})
	}
}

func TestNewUploadManagerGCSRequiresCredentials(t *testing.T) {
	t.Setenv("GCS_ACCESS_KEY_ID", "")
	t.Setenv("GCS_SECRET_ACCESS_KEY", "")

	manager, err := newUploadManager(t.Context(), &Config{
		S3Uploader: S3UploaderConfig{
			Region:   "auto",
			S3Bucket: "bucket",
			Endpoint: "https://storage.googleapis.com",
		},
	}, zap.NewNop(), "logs", "otlp", false)

	assert.Nil(t, manager)
	assert.EqualError(t, err, "GCS endpoint requires GCS_ACCESS_KEY_ID and GCS_SECRET_ACCESS_KEY environment variables")
}

func TestNewUploadManagerGCSAcceptsDedicatedCredentials(t *testing.T) {
	t.Setenv("GCS_ACCESS_KEY_ID", "access-key")
	t.Setenv("GCS_SECRET_ACCESS_KEY", "secret-key")
	// Prevent the AWS SDK from consulting a developer's local config while the
	// base config is initialized before the GCS provider replaces it.
	t.Setenv("AWS_CONFIG_FILE", os.DevNull)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", os.DevNull)

	manager, err := newUploadManager(t.Context(), &Config{
		S3Uploader: S3UploaderConfig{
			Region:   "auto",
			S3Bucket: "bucket",
			Endpoint: "https://storage.googleapis.com",
		},
	}, zap.NewNop(), "logs", "otlp", false)

	assert.NoError(t, err)
	assert.NotNil(t, manager)
}
