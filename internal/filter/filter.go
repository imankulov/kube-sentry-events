package filter

import (
	"github.com/getsentry/sentry-go"
	corev1 "k8s.io/api/core/v1"
)

// Filter determines which Kubernetes events should be sent to Sentry.
type Filter struct {
	namespaces        map[string]struct{}
	excludeNamespaces map[string]struct{}
	eventReasons      map[string]struct{}
	eventThresholds   map[string]int32
	severityMap       map[string]sentry.Level
}

// New creates a new event filter.
func New(namespaces, excludeNamespaces, eventReasons []string, thresholds map[string]int32) *Filter {
	f := &Filter{
		namespaces:        toSet(namespaces),
		excludeNamespaces: toSet(excludeNamespaces),
		eventReasons:      toSet(eventReasons),
		eventThresholds:   thresholds,
		severityMap:       defaultSeverityMap(),
	}
	return f
}

// ShouldProcess returns true if the event should be processed.
// This checks namespace and event type filters, but NOT thresholds.
// Use MeetsThreshold separately to check count thresholds.
func (f *Filter) ShouldProcess(event *corev1.Event) bool {
	// Filter by namespace
	ns := event.InvolvedObject.Namespace
	if ns == "" {
		ns = event.Namespace
	}

	// If specific namespaces are configured, only allow those
	if len(f.namespaces) > 0 {
		if _, ok := f.namespaces[ns]; !ok {
			return false
		}
	}

	// Check exclude list
	if _, excluded := f.excludeNamespaces[ns]; excluded {
		return false
	}

	// Filter by event reason
	if _, ok := f.eventReasons[event.Reason]; !ok {
		return false
	}

	// Only process Warning events (Normal events are informational)
	if event.Type != corev1.EventTypeWarning {
		return false
	}

	return true
}

// MeetsThreshold returns true if the event's count meets the minimum threshold.
// Events below the threshold are considered transient and should be skipped.
func (f *Filter) MeetsThreshold(event *corev1.Event) bool {
	threshold, ok := f.eventThresholds[event.Reason]
	if !ok {
		// No threshold configured, allow by default
		return true
	}

	// Use the k8s event count (how many times k8s has seen this event)
	return event.Count >= threshold
}

// GetThreshold returns the threshold for an event reason.
func (f *Filter) GetThreshold(reason string) int32 {
	if threshold, ok := f.eventThresholds[reason]; ok {
		return threshold
	}
	return 1
}

// GetSeverity returns the Sentry severity level for an event reason.
func (f *Filter) GetSeverity(reason string) sentry.Level {
	if level, ok := f.severityMap[reason]; ok {
		return level
	}
	return sentry.LevelWarning
}

func defaultSeverityMap() map[string]sentry.Level {
	return map[string]sentry.Level{
		// Error level - critical issues
		"OOMKilled":          sentry.LevelError,
		"CrashLoopBackOff":   sentry.LevelError,
		"FailedScheduling":   sentry.LevelError,
		"Evicted":            sentry.LevelError,
		"FailedMount":        sentry.LevelError,
		"FailedAttachVolume": sentry.LevelError,
		"ImagePullBackOff":   sentry.LevelError,
		"ErrImagePull":       sentry.LevelError,
		"FailedCreate":       sentry.LevelError,

		// Warning level - issues that may self-resolve
		"Unhealthy":    sentry.LevelWarning,
		"BackOff":      sentry.LevelWarning,
		"Killing":      sentry.LevelWarning,
		"NodeNotReady": sentry.LevelWarning,
		"FailedSync":   sentry.LevelWarning,

		// Info level - informational
		"NodeReady": sentry.LevelInfo,
	}
}

func toSet(slice []string) map[string]struct{} {
	set := make(map[string]struct{}, len(slice))
	for _, s := range slice {
		set[s] = struct{}{}
	}
	return set
}
