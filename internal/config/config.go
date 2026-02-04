package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds the application configuration.
type Config struct {
	// Sentry configuration
	SentryDSN         string
	SentryEnvironment string

	// Namespace filtering
	Namespaces        []string // Empty means all namespaces
	ExcludeNamespaces []string

	// Event filtering
	EventReasons []string

	// Thresholds - minimum k8s event count before creating Sentry Issues
	// Events below threshold still go to Sentry Logs for observability
	EventThresholds map[string]int32

	// Enable Sentry Logs for all events (observability mode)
	EnableLogs bool

	// Deduplication
	DedupWindow time.Duration

	// Logging
	LogLevel string
}

// DefaultEventReasons returns the default list of event reasons to monitor.
// Note: Only Warning-type events are processed; Normal events like "Killing" are excluded.
func DefaultEventReasons() []string {
	return []string{
		// High priority - always critical
		"OOMKilled",
		"CrashLoopBackOff",
		"FailedScheduling",
		"Evicted",
		"FailedMount",
		"FailedAttachVolume",
		// Medium priority - may be transient
		"Unhealthy",
		"ImagePullBackOff",
		"ErrImagePull",
		"BackOff",
		"FailedCreate",
	}
}

// DefaultEventThresholds returns the minimum k8s event count before sending to Sentry.
// Events below these thresholds are considered transient and filtered out.
// A threshold of 1 means send immediately (no filtering).
func DefaultEventThresholds() map[string]int32 {
	return map[string]int32{
		// Always send immediately - these are critical
		"OOMKilled":          1,
		"CrashLoopBackOff":   1,
		"Evicted":            1,
		"FailedScheduling":   1,
		"FailedMount":        1,
		"FailedAttachVolume": 1,

		// Require multiple occurrences - often transient during startup/deployment
		"Unhealthy":        5, // Probe failures are common during rolling updates
		"BackOff":          3, // Container restarts may be temporary
		"ImagePullBackOff": 3, // May be temporary registry issues
		"ErrImagePull":     2, // Usually persistent, but give one retry
		"FailedCreate":     2, // May be temporary resource constraints
	}
}

// Load reads configuration from environment variables.
// If dryRun is true, SENTRY_DSN is not required.
func Load(dryRun bool) (*Config, error) {
	cfg := &Config{
		SentryDSN:         os.Getenv("SENTRY_DSN"),
		SentryEnvironment: getEnvOrDefault("SENTRY_ENVIRONMENT", "production"),
		LogLevel:          getEnvOrDefault("KUBE_SENTRY_LOG_LEVEL", "info"),
	}

	// Validate required fields (skip in dry-run mode)
	if !dryRun && cfg.SentryDSN == "" {
		return nil, fmt.Errorf("SENTRY_DSN environment variable is required (use --dry-run to skip)")
	}

	// Parse namespaces
	if ns := os.Getenv("KUBE_SENTRY_NAMESPACES"); ns != "" {
		cfg.Namespaces = splitAndTrim(ns)
	}

	if excludeNs := os.Getenv("KUBE_SENTRY_EXCLUDE_NAMESPACES"); excludeNs != "" {
		cfg.ExcludeNamespaces = splitAndTrim(excludeNs)
	} else {
		cfg.ExcludeNamespaces = []string{"kube-system"}
	}

	// Parse event reasons
	if events := os.Getenv("KUBE_SENTRY_EVENTS"); events != "" {
		cfg.EventReasons = splitAndTrim(events)
	} else {
		cfg.EventReasons = DefaultEventReasons()
	}

	// Parse event thresholds (format: "Reason:count,Reason:count")
	cfg.EventThresholds = DefaultEventThresholds()
	if thresholds := os.Getenv("KUBE_SENTRY_THRESHOLDS"); thresholds != "" {
		for _, item := range splitAndTrim(thresholds) {
			parts := strings.SplitN(item, ":", 2)
			if len(parts) == 2 {
				reason := strings.TrimSpace(parts[0])
				countStr := strings.TrimSpace(parts[1])
				count, err := parseThreshold(countStr)
				if err != nil {
					return nil, fmt.Errorf("invalid threshold for %s: %w", reason, err)
				}
				cfg.EventThresholds[reason] = count
			}
		}
	}

	// Parse enable logs (default: true for observability)
	enableLogsStr := getEnvOrDefault("KUBE_SENTRY_ENABLE_LOGS", "true")
	cfg.EnableLogs = enableLogsStr == "true" || enableLogsStr == "1"

	// Parse dedup window
	dedupStr := getEnvOrDefault("KUBE_SENTRY_DEDUP_WINDOW", "5m")
	dedupWindow, err := time.ParseDuration(dedupStr)
	if err != nil {
		return nil, fmt.Errorf("invalid KUBE_SENTRY_DEDUP_WINDOW: %w", err)
	}
	cfg.DedupWindow = dedupWindow

	return cfg, nil
}

func parseThreshold(s string) (int32, error) {
	var result int32
	n, err := fmt.Sscanf(s, "%d", &result)
	if err != nil || n != 1 {
		return 0, fmt.Errorf("expected integer, got %q", s)
	}
	return result, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
