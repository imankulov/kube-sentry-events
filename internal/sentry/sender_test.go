package sentry

import (
	"testing"
)

func TestExtractDeploymentName(t *testing.T) {
	tests := []struct {
		podName    string
		deployment string
	}{
		// Standard deployment pod names
		{"worker-79c6dd4b57-wcdzt", "worker"},
		{"api-server-5d8f9b7c4d-abc12", "api-server"},
		{"my-app-6b8f9d7c5e-xyz99", "my-app"},

		// StatefulSet or simple names (no extraction)
		{"redis-0", "redis-0"},
		{"postgres-1", "postgres-1"},

		// Single word (no dashes)
		{"standalone", "standalone"},

		// Edge cases
		{"a-b-c", "a-b-c"},         // Not matching pattern
		{"app-12345-abcde", "app"}, // Matches pattern
		{"my-complex-app-name-abc123def-x1y2z", "my-complex-app-name"},
	}

	for _, tt := range tests {
		t.Run(tt.podName, func(t *testing.T) {
			got := ExtractDeploymentName(tt.podName)
			if got != tt.deployment {
				t.Errorf("ExtractDeploymentName(%q) = %q, want %q", tt.podName, got, tt.deployment)
			}
		})
	}
}

func TestIsAlphanumeric(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"abc123", true},
		{"abcdef", true},
		{"123456", true},
		{"", true},         // empty string is alphanumeric
		{"ABC", false},     // uppercase
		{"abc-123", false}, // dash
		{"abc_123", false}, // underscore
		{"abc 123", false}, // space
		{"abc.123", false}, // dot
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isAlphanumeric(tt.input)
			if got != tt.expected {
				t.Errorf("isAlphanumeric(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
