// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awsutil

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

const defaultSharedCredentialsFileProfile = "default"

var (
	sharedCredentialsFileRefreshInterval = 5 * time.Minute
	sharedCredentialsFileFallbackRefresh = time.Minute
)

type sharedCredentialsFileProvider struct {
	filename        string
	profile         string
	refreshInterval time.Duration
	fallbackRefresh time.Duration

	mu       sync.Mutex
	lastGood aws.Credentials
}

func newSharedCredentialsFileProvider(filename string) *sharedCredentialsFileProvider {
	return &sharedCredentialsFileProvider{
		filename:        filename,
		profile:         defaultSharedCredentialsFileProfile,
		refreshInterval: sharedCredentialsFileRefreshInterval,
		fallbackRefresh: sharedCredentialsFileFallbackRefresh,
	}
}

func (p *sharedCredentialsFileProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	creds, err := p.load(ctx)
	if err == nil {
		p.lastGood = creds
		return creds, nil
	}

	if p.lastGood.HasKeys() {
		creds = p.lastGood
		creds.Expires = time.Now().Add(p.fallbackRefresh)
		return creds, nil
	}

	return aws.Credentials{}, err
}

func (p *sharedCredentialsFileProvider) load(ctx context.Context) (aws.Credentials, error) {
	cfg, err := config.LoadSharedConfigProfile(ctx, p.profile, func(o *config.LoadSharedConfigOptions) {
		o.ConfigFiles = []string{}
		o.CredentialsFiles = []string{p.filename}
	})
	if err != nil {
		return aws.Credentials{}, err
	}

	creds := cfg.Credentials
	if !creds.HasKeys() {
		return aws.Credentials{}, fmt.Errorf("shared credentials file %q profile %q does not contain access key and secret key", p.filename, p.profile)
	}

	creds.CanExpire = true
	creds.Expires = time.Now().Add(p.refreshInterval)
	return creds, nil
}
