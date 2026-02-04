package config

import (
	"testing"
	"time"
)

func TestLoad_RequiresSentryDSN(t *testing.T) {
	// Clear any existing env
	t.Setenv("SENTRY_DSN", "")

	_, err := Load(false)
	if err == nil {
		t.Error("expected error when SENTRY_DSN is not set")
	}
}

func TestLoad_DryRunSkipsDSNValidation(t *testing.T) {
	t.Setenv("SENTRY_DSN", "")

	cfg, err := Load(true)
	if err != nil {
		t.Errorf("expected no error in dry-run mode, got %v", err)
	}
	if cfg == nil {
		t.Error("expected config to be returned in dry-run mode")
	}
}

func TestLoad_DefaultValues(t *testing.T) {
	t.Setenv("SENTRY_DSN", "https://test@sentry.io/123")
	t.Setenv("SENTRY_ENVIRONMENT", "")
	t.Setenv("KUBE_SENTRY_NAMESPACES", "")
	t.Setenv("KUBE_SENTRY_EXCLUDE_NAMESPACES", "")
	t.Setenv("KUBE_SENTRY_EVENTS", "")
	t.Setenv("KUBE_SENTRY_DEDUP_WINDOW", "")
	t.Setenv("KUBE_SENTRY_LOG_LEVEL", "")

	cfg, err := Load(false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.SentryDSN != "https://test@sentry.io/123" {
		t.Errorf("expected SentryDSN to be set, got %s", cfg.SentryDSN)
	}

	if cfg.SentryEnvironment != "production" {
		t.Errorf("expected default environment 'production', got %s", cfg.SentryEnvironment)
	}

	if cfg.DedupWindow != 5*time.Minute {
		t.Errorf("expected default dedup window 5m, got %v", cfg.DedupWindow)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("expected default log level 'info', got %s", cfg.LogLevel)
	}

	if len(cfg.Namespaces) != 0 {
		t.Errorf("expected empty namespaces by default, got %v", cfg.Namespaces)
	}

	if len(cfg.ExcludeNamespaces) != 1 || cfg.ExcludeNamespaces[0] != "kube-system" {
		t.Errorf("expected default exclude namespaces [kube-system], got %v", cfg.ExcludeNamespaces)
	}

	if len(cfg.EventReasons) == 0 {
		t.Error("expected default event reasons to be set")
	}
}

func TestLoad_CustomValues(t *testing.T) {
	t.Setenv("SENTRY_DSN", "https://custom@sentry.io/456")
	t.Setenv("SENTRY_ENVIRONMENT", "staging")
	t.Setenv("KUBE_SENTRY_NAMESPACES", "default, production")
	t.Setenv("KUBE_SENTRY_EXCLUDE_NAMESPACES", "kube-system, monitoring")
	t.Setenv("KUBE_SENTRY_EVENTS", "OOMKilled, CrashLoopBackOff")
	t.Setenv("KUBE_SENTRY_DEDUP_WINDOW", "10m")
	t.Setenv("KUBE_SENTRY_LOG_LEVEL", "debug")

	cfg, err := Load(false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.SentryEnvironment != "staging" {
		t.Errorf("expected environment 'staging', got %s", cfg.SentryEnvironment)
	}

	if cfg.DedupWindow != 10*time.Minute {
		t.Errorf("expected dedup window 10m, got %v", cfg.DedupWindow)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("expected log level 'debug', got %s", cfg.LogLevel)
	}

	expectedNamespaces := []string{"default", "production"}
	if len(cfg.Namespaces) != len(expectedNamespaces) {
		t.Errorf("expected namespaces %v, got %v", expectedNamespaces, cfg.Namespaces)
	}

	expectedExclude := []string{"kube-system", "monitoring"}
	if len(cfg.ExcludeNamespaces) != len(expectedExclude) {
		t.Errorf("expected exclude namespaces %v, got %v", expectedExclude, cfg.ExcludeNamespaces)
	}

	expectedEvents := []string{"OOMKilled", "CrashLoopBackOff"}
	if len(cfg.EventReasons) != len(expectedEvents) {
		t.Errorf("expected events %v, got %v", expectedEvents, cfg.EventReasons)
	}
}

func TestLoad_InvalidDedupWindow(t *testing.T) {
	t.Setenv("SENTRY_DSN", "https://test@sentry.io/123")
	t.Setenv("KUBE_SENTRY_DEDUP_WINDOW", "invalid")

	_, err := Load(false)
	if err == nil {
		t.Error("expected error for invalid dedup window")
	}
}

func TestDefaultEventReasons(t *testing.T) {
	reasons := DefaultEventReasons()

	if len(reasons) == 0 {
		t.Error("expected default event reasons to be non-empty")
	}

	// Check that critical events are included
	expected := map[string]bool{
		"OOMKilled":        false,
		"CrashLoopBackOff": false,
		"FailedScheduling": false,
		"ImagePullBackOff": false,
	}

	for _, r := range reasons {
		if _, ok := expected[r]; ok {
			expected[r] = true
		}
	}

	for event, found := range expected {
		if !found {
			t.Errorf("expected %s to be in default event reasons", event)
		}
	}
}
