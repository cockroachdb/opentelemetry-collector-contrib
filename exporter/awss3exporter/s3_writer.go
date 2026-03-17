// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awss3exporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/awss3exporter"

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/awss3exporter/internal/upload"
)

// RecalculateV4Signature is an HTTP round-tripper middleware that re-signs
// requests for GCS's S3-compatible API. GCS rejects requests containing
// AWS-specific headers (X-Amz-Storage-Class, Content-Encoding) in the
// signature, so this middleware strips them and re-signs before sending.
type RecalculateV4Signature struct {
	next   http.RoundTripper
	signer *v4.Signer
	cfg    aws.Config
}

func (lt *RecalculateV4Signature) RoundTrip(req *http.Request) (*http.Response, error) {
	acceptEncoding := req.Header.Get("Accept-Encoding")
	req.Header.Del("Accept-Encoding")
	req.Header.Del("Content-Encoding")
	req.Header.Del("X-Amz-Storage-Class")

	timeString := req.Header.Get("X-Amz-Date")
	timeDate, err := time.Parse("20060102T150405Z", timeString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse X-Amz-Date header %q: %w", timeString, err)
	}

	creds, err := lt.cfg.Credentials.Retrieve(req.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve credentials for GCS re-signing: %w", err)
	}

	if err := lt.signer.SignHTTP(req.Context(), creds, req, v4.GetPayloadHash(req.Context()), "s3", lt.cfg.Region, timeDate); err != nil {
		return nil, err
	}

	req.Header.Set("Accept-Encoding", acceptEncoding)
	return lt.next.RoundTrip(req)
}

func newUploadManager(
	ctx context.Context,
	conf *Config,
	metadata string,
	format string,
	isCompressed bool,
) (upload.Manager, error) {
	configOpts := []func(*config.LoadOptions) error{}

	if region := conf.S3Uploader.Region; region != "" {
		configOpts = append(configOpts, config.WithRegion(region))
	}

	switch conf.S3Uploader.RetryMode {
	case "nop":
		configOpts = append(configOpts, config.WithRetryer(func() aws.Retryer {
			return aws.NopRetryer{}
		}))
	default:
		configOpts = append(configOpts, config.WithRetryMode(aws.RetryMode(conf.S3Uploader.RetryMode)))
	}

	cfg, err := config.LoadDefaultConfig(ctx, configOpts...)
	if err != nil {
		return nil, err
	}

	s3Opts := []func(*s3.Options){
		func(o *s3.Options) {
			o.EndpointOptions = s3.EndpointResolverOptions{
				DisableHTTPS: conf.S3Uploader.DisableSSL,
			}
			o.UsePathStyle = conf.S3Uploader.S3ForcePathStyle
			o.Retryer = retry.AddWithMaxAttempts(o.Retryer, conf.S3Uploader.RetryMaxAttempts)
			o.Retryer = retry.AddWithMaxBackoffDelay(o.Retryer, conf.S3Uploader.RetryMaxBackoff)
		},
	}

	if conf.S3Uploader.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(conf.S3Uploader.Endpoint)
		})
	}

	if arn := conf.S3Uploader.RoleArn; arn != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.Credentials = stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), arn)
		})
	}

	if endpoint := conf.S3Uploader.Endpoint; endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}

	var managerOpts []upload.ManagerOpt
	if conf.S3Uploader.ACL != "" {
		managerOpts = append(managerOpts,
			upload.WithACL(s3types.ObjectCannedACL(conf.S3Uploader.ACL)))
	}

	var uniqueKeyFunc func() string
	switch conf.S3Uploader.UniqueKeyFuncName {
	case "uuidv7":
		uniqueKeyFunc = upload.GenerateUUIDv7
	default:
		uniqueKeyFunc = nil
	}

	var s3PartitionTimeLocation *time.Location
	if conf.S3Uploader.S3PartitionTimezone != "" {
		s3PartitionTimeLocation, err = time.LoadLocation(conf.S3Uploader.S3PartitionTimezone)
		if err != nil {
			return nil, fmt.Errorf("invalid S3 partition timezone: %w", err)
		}
	} else {
		s3PartitionTimeLocation = time.Local
	}

	// When targeting GCS via its S3-compatible API, use dedicated GCS HMAC
	// credentials (GCS_ACCESS_KEY_ID / GCS_SECRET_ACCESS_KEY) instead of the
	// default AWS credential chain. This prevents conflicts with AWS IRSA
	// credentials when both S3 and GCS exporters run in the same pod.
	// The HTTP client is wrapped to strip AWS-specific headers and re-sign
	// requests for GCS compatibility.
	if conf.S3Uploader.Endpoint == "https://storage.googleapis.com" {
		gcsAccessKey := os.Getenv("GCS_ACCESS_KEY_ID")
		gcsSecretKey := os.Getenv("GCS_SECRET_ACCESS_KEY")
		if gcsAccessKey == "" || gcsSecretKey == "" {
			return nil, fmt.Errorf("GCS endpoint requires GCS_ACCESS_KEY_ID and GCS_SECRET_ACCESS_KEY environment variables")
		}
		cfg.Credentials = credentials.NewStaticCredentialsProvider(gcsAccessKey, gcsSecretKey, "")

		cfg.HTTPClient = &http.Client{
			Transport: &RecalculateV4Signature{
				next:   http.DefaultTransport,
				signer: v4.NewSigner(),
				cfg:    cfg,
			},
		}
		cfg.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	}

	return upload.NewS3Manager(
		conf.S3Uploader.S3Bucket,
		&upload.PartitionKeyBuilder{
			PartitionBasePrefix:   conf.S3Uploader.S3BasePrefix,
			PartitionPrefix:       conf.S3Uploader.S3Prefix,
			PartitionFormat:       conf.S3Uploader.S3PartitionFormat,
			PartitionTimeLocation: s3PartitionTimeLocation,
			FilePrefix:            conf.S3Uploader.FilePrefix,
			FileFormat:            format,
			Metadata:              metadata,
			Compression:           conf.S3Uploader.Compression,
			UniqueKeyFunc:         uniqueKeyFunc,
			IsCompressed:          isCompressed,
		},
		s3.NewFromConfig(cfg, s3Opts...),
		s3types.StorageClass(conf.S3Uploader.StorageClass),
		managerOpts...,
	), nil
}
