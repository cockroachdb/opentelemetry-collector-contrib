// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awscloudwatchlogsexporter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestReplacePatternValidTaskId(t *testing.T) {
	logger := zap.NewNop()

	input := "{TaskId}"

	attrMap := map[string]any{
		"aws.ecs.cluster.name": "test-cluster-name",
		"aws.ecs.task.id":      "test-task-id",
	}

	s, success := replacePatterns(input, anyMapToStringMap(attrMap), nil, logger)

	assert.Equal(t, "test-task-id", s)
	assert.True(t, success)
}

func TestReplacePatternValidServiceName(t *testing.T) {
	logger := zap.NewNop()

	input := "{ServiceName}"

	attrMap := map[string]any{
		"service.name": "some-test-service",
	}

	s, success := replacePatterns(input, anyMapToStringMap(attrMap), nil, logger)

	assert.Equal(t, "some-test-service", s)
	assert.True(t, success)
}

func TestReplacePatternValidClusterName(t *testing.T) {
	logger := zap.NewNop()

	input := "/aws/ecs/containerinsights/{ClusterName}/performance"

	attrMap := map[string]any{
		"aws.ecs.cluster.name": "test-cluster-name",
		"aws.ecs.task.id":      "test-task-id",
	}

	s, success := replacePatterns(input, anyMapToStringMap(attrMap), nil, logger)

	assert.Equal(t, "/aws/ecs/containerinsights/test-cluster-name/performance", s)
	assert.True(t, success)
}

func TestReplacePatternMissingAttribute(t *testing.T) {
	logger := zap.NewNop()

	input := "/aws/ecs/containerinsights/{ClusterName}/performance"

	attrMap := map[string]any{
		"aws.ecs.task.id": "test-task-id",
	}

	s, success := replacePatterns(input, anyMapToStringMap(attrMap), nil, logger)

	assert.Equal(t, "/aws/ecs/containerinsights/undefined/performance", s)
	assert.False(t, success)
}

func TestReplacePatternValidPodName(t *testing.T) {
	logger := zap.NewNop()

	input := "/aws/eks/containerinsights/{PodName}/performance"

	attrMap := map[string]any{
		"aws.eks.cluster.name": "test-cluster-name",
		"PodName":              "test-pod-001",
	}

	s, success := replacePatterns(input, anyMapToStringMap(attrMap), nil, logger)

	assert.Equal(t, "/aws/eks/containerinsights/test-pod-001/performance", s)
	assert.True(t, success)
}

func TestReplacePatternValidPod(t *testing.T) {
	logger := zap.NewNop()

	input := "/aws/eks/containerinsights/{PodName}/performance"

	attrMap := map[string]any{
		"aws.eks.cluster.name": "test-cluster-name",
		"PodName":              "test-pod-001",
	}

	s, success := replacePatterns(input, anyMapToStringMap(attrMap), nil, logger)

	assert.Equal(t, "/aws/eks/containerinsights/test-pod-001/performance", s)
	assert.True(t, success)
}

func TestReplacePatternMissingPodName(t *testing.T) {
	logger := zap.NewNop()

	input := "/aws/eks/containerinsights/{PodName}/performance"

	attrMap := map[string]any{
		"aws.eks.cluster.name": "test-cluster-name",
	}

	s, success := replacePatterns(input, anyMapToStringMap(attrMap), nil, logger)

	assert.Equal(t, "/aws/eks/containerinsights/undefined/performance", s)
	assert.False(t, success)
}

func TestReplacePatternAttrPlaceholderClusterName(t *testing.T) {
	logger := zap.NewNop()

	input := "/aws/ecs/containerinsights/{ClusterName}/performance"

	attrMap := map[string]any{
		"ClusterName": "test-cluster-name",
	}

	s, success := replacePatterns(input, anyMapToStringMap(attrMap), nil, logger)

	assert.Equal(t, "/aws/ecs/containerinsights/test-cluster-name/performance", s)
	assert.True(t, success)
}

func TestReplacePatternWrongKey(t *testing.T) {
	logger := zap.NewNop()

	input := "/aws/ecs/containerinsights/{WrongKey}/performance"

	attrMap := map[string]any{
		"ClusterName": "test-task-id",
	}

	s, success := replacePatterns(input, anyMapToStringMap(attrMap), nil, logger)

	assert.Equal(t, "/aws/ecs/containerinsights/{WrongKey}/performance", s)
	assert.True(t, success)
}

func TestReplacePatternNilAttrValue(t *testing.T) {
	logger := zap.NewNop()

	input := "/aws/ecs/containerinsights/{ClusterName}/performance"

	attrMap := map[string]any{
		"ClusterName": "",
	}

	s, success := replacePatterns(input, anyMapToStringMap(attrMap), nil, logger)

	assert.Equal(t, "/aws/ecs/containerinsights/undefined/performance", s)
	assert.False(t, success)
}

func TestReplacePatternValidTaskDefinitionFamily(t *testing.T) {
	logger := zap.NewNop()

	input := "{TaskDefinitionFamily}"

	attrMap := map[string]any{
		"aws.ecs.cluster.name": "test-cluster-name",
		"aws.ecs.task.family":  "test-task-definition-family",
	}

	s, success := replacePatterns(input, anyMapToStringMap(attrMap), nil, logger)

	assert.Equal(t, "test-task-definition-family", s)
	assert.True(t, success)
}

// TestReplacePatternEmptyPatternValue exercises the EmptyPatternValue
// override: when set on Config, missing/empty attributes substitute the
// user-supplied string instead of the historical "undefined".
func TestReplacePatternEmptyPatternValue(t *testing.T) {
	logger := zap.NewNop()
	emptyString := ""
	fallback := "FALLBACK"

	tests := []struct {
		name       string
		input      string
		attrMap    map[string]any
		emptyValue *string
		want       string
		wantOK     bool
	}{
		{
			name:       "nil emptyValue preserves undefined (missing attr)",
			input:      "prefix-{ClusterName}-suffix",
			attrMap:    map[string]any{},
			emptyValue: nil,
			want:       "prefix-undefined-suffix",
			wantOK:     false,
		},
		{
			name:       "nil emptyValue preserves undefined (empty attr)",
			input:      "prefix-{ClusterName}-suffix",
			attrMap:    map[string]any{"ClusterName": ""},
			emptyValue: nil,
			want:       "prefix-undefined-suffix",
			wantOK:     false,
		},
		{
			name:       "explicit empty string drops placeholder (missing attr)",
			input:      "prefix-{ClusterName}-suffix",
			attrMap:    map[string]any{},
			emptyValue: &emptyString,
			want:       "prefix--suffix",
			wantOK:     false,
		},
		{
			name:       "explicit empty string drops placeholder (empty attr)",
			input:      "prefix-{ClusterName}-suffix",
			attrMap:    map[string]any{"ClusterName": ""},
			emptyValue: &emptyString,
			want:       "prefix--suffix",
			wantOK:     false,
		},
		{
			name:       "custom fallback substitutes user value (missing attr)",
			input:      "prefix-{ClusterName}-suffix",
			attrMap:    map[string]any{},
			emptyValue: &fallback,
			want:       "prefix-FALLBACK-suffix",
			wantOK:     false,
		},
		{
			name:       "emptyValue ignored when attr is present",
			input:      "prefix-{ClusterName}-suffix",
			attrMap:    map[string]any{"aws.ecs.cluster.name": "real-cluster"},
			emptyValue: &emptyString,
			want:       "prefix-real-cluster-suffix",
			wantOK:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := replacePatterns(tc.input, anyMapToStringMap(tc.attrMap), tc.emptyValue, logger)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantOK, ok)
		})
	}
}

// TestGetLogInfoEmptyPatternValue verifies Config.EmptyPatternValue threads
// correctly through getLogInfo to control the final log_group/log_stream output.
func TestGetLogInfoEmptyPatternValue(t *testing.T) {
	emptyString := ""

	tests := []struct {
		name           string
		groupTemplate  string
		streamTemplate string
		attrMap        map[string]any
		emptyValue     *string
		wantGroup      string
		wantStream     string
	}{
		{
			name:           "nil emptyValue: undefined for missing attrs",
			groupTemplate:  "logs/{ClusterName}",
			streamTemplate: "stream-{NodeName}",
			attrMap:        map[string]any{},
			emptyValue:     nil,
			wantGroup:      "logs/undefined",
			wantStream:     "stream-undefined",
		},
		{
			name:           "explicit empty: placeholder dropped",
			groupTemplate:  "logs/{ClusterName}",
			streamTemplate: "stream.n{InstanceId}",
			attrMap:        map[string]any{},
			emptyValue:     &emptyString,
			wantGroup:      "logs/",
			wantStream:     "stream.n",
		},
		{
			name:           "explicit empty: present attrs still substituted",
			groupTemplate:  "logs/{ClusterName}",
			streamTemplate: "stream.{ServiceName}.n{InstanceId}",
			attrMap: map[string]any{
				"aws.ecs.cluster.name": "prod",
				"service.name":         "STORAGE",
				// service.instance.id missing on purpose
			},
			emptyValue: &emptyString,
			wantGroup:  "logs/prod",
			wantStream: "stream.STORAGE.n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := &Config{
				LogGroupName:      tc.groupTemplate,
				LogStreamName:     tc.streamTemplate,
				EmptyPatternValue: tc.emptyValue,
				logger:            zap.NewNop(),
			}
			gotGroup, gotStream, _ := getLogInfo(tc.attrMap, config)
			assert.Equal(t, tc.wantGroup, gotGroup)
			assert.Equal(t, tc.wantStream, gotStream)
		})
	}
}

func TestIsPatternValid(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		expected bool
	}{
		{
			name:     "no curly brackets",
			pattern:  "example-string",
			expected: true,
		},
		{
			name:     "valid single pattern",
			pattern:  "prefix-{ClusterName}-suffix",
			expected: true,
		},
		{
			name:     "valid multiple patterns",
			pattern:  "{ServiceName}-{TaskId}-{FaasName}",
			expected: true,
		},
		{
			name:     "invalid pattern key",
			pattern:  "prefix-{RandomName}-suffix",
			expected: false,
		},
		{
			name:     "mixed valid and invalid",
			pattern:  "{ClusterName}-{RandomName}",
			expected: false,
		},
		{
			name:     "empty curly brackets",
			pattern:  "prefix-{}-suffix",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := isPatternValid(tc.pattern)
			assert.Equal(t, tc.expected, result, "Pattern: %s", tc.pattern)
		})
	}
}
