package filter

import (
	"testing"

	"github.com/getsentry/sentry-go"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newTestEvent(namespace, name, reason, eventType string) *corev1.Event {
	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Namespace: namespace,
			Name:      name,
		},
		Reason: reason,
		Type:   eventType,
		Count:  1,
	}
}

func newTestEventWithCount(namespace, name, reason, eventType string, count int32) *corev1.Event {
	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Namespace: namespace,
			Name:      name,
		},
		Reason: reason,
		Type:   eventType,
		Count:  count,
	}
}

func defaultThresholds() map[string]int32 {
	return map[string]int32{
		"OOMKilled":        1,
		"CrashLoopBackOff": 1,
		"Unhealthy":        5,
	}
}

func TestFilter_ShouldProcess_AllowedEvent(t *testing.T) {
	f := New(nil, []string{"kube-system"}, []string{"OOMKilled", "CrashLoopBackOff"}, defaultThresholds())

	event := newTestEvent("default", "my-pod", "OOMKilled", corev1.EventTypeWarning)

	if !f.ShouldProcess(event) {
		t.Error("expected event to be processed")
	}
}

func TestFilter_ShouldProcess_ExcludedNamespace(t *testing.T) {
	f := New(nil, []string{"kube-system"}, []string{"OOMKilled"}, defaultThresholds())

	event := newTestEvent("kube-system", "my-pod", "OOMKilled", corev1.EventTypeWarning)

	if f.ShouldProcess(event) {
		t.Error("expected event in excluded namespace to be filtered out")
	}
}

func TestFilter_ShouldProcess_SpecificNamespaces(t *testing.T) {
	f := New([]string{"production", "staging"}, nil, []string{"OOMKilled"}, defaultThresholds())

	// Event in allowed namespace
	event1 := newTestEvent("production", "my-pod", "OOMKilled", corev1.EventTypeWarning)
	if !f.ShouldProcess(event1) {
		t.Error("expected event in allowed namespace to be processed")
	}

	// Event in non-allowed namespace
	event2 := newTestEvent("development", "my-pod", "OOMKilled", corev1.EventTypeWarning)
	if f.ShouldProcess(event2) {
		t.Error("expected event in non-allowed namespace to be filtered out")
	}
}

func TestFilter_ShouldProcess_UnknownReason(t *testing.T) {
	f := New(nil, nil, []string{"OOMKilled", "CrashLoopBackOff"}, defaultThresholds())

	event := newTestEvent("default", "my-pod", "Scheduled", corev1.EventTypeWarning)

	if f.ShouldProcess(event) {
		t.Error("expected event with unknown reason to be filtered out")
	}
}

func TestFilter_ShouldProcess_NormalEventType(t *testing.T) {
	f := New(nil, nil, []string{"OOMKilled"}, defaultThresholds())

	// Normal events should be filtered out (we only want Warning events)
	event := newTestEvent("default", "my-pod", "OOMKilled", corev1.EventTypeNormal)

	if f.ShouldProcess(event) {
		t.Error("expected Normal event type to be filtered out")
	}
}

func TestFilter_GetSeverity(t *testing.T) {
	f := New(nil, nil, []string{"OOMKilled", "Unhealthy", "NodeReady"}, defaultThresholds())

	tests := []struct {
		reason   string
		expected sentry.Level
	}{
		{"OOMKilled", sentry.LevelError},
		{"CrashLoopBackOff", sentry.LevelError},
		{"FailedScheduling", sentry.LevelError},
		{"ImagePullBackOff", sentry.LevelError},
		{"Unhealthy", sentry.LevelWarning},
		{"BackOff", sentry.LevelWarning},
		{"NodeReady", sentry.LevelInfo},
		{"Unknown", sentry.LevelWarning}, // default
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			got := f.GetSeverity(tt.reason)
			if got != tt.expected {
				t.Errorf("GetSeverity(%s) = %v, want %v", tt.reason, got, tt.expected)
			}
		})
	}
}

func TestFilter_EmptyNamespaceInEvent(t *testing.T) {
	f := New(nil, []string{"kube-system"}, []string{"OOMKilled"}, defaultThresholds())

	// Event with namespace only in ObjectMeta
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		InvolvedObject: corev1.ObjectReference{
			Name: "my-pod",
			// Namespace intentionally empty
		},
		Reason: "OOMKilled",
		Type:   corev1.EventTypeWarning,
		Count:  1,
	}

	if !f.ShouldProcess(event) {
		t.Error("expected event to use ObjectMeta namespace as fallback")
	}
}

func TestFilter_MeetsThreshold(t *testing.T) {
	thresholds := map[string]int32{
		"OOMKilled": 1,
		"Unhealthy": 5,
	}
	f := New(nil, nil, []string{"OOMKilled", "Unhealthy"}, thresholds)

	// OOMKilled with count 1 should meet threshold (threshold is 1)
	event1 := newTestEventWithCount("default", "pod1", "OOMKilled", corev1.EventTypeWarning, 1)
	if !f.MeetsThreshold(event1) {
		t.Error("expected OOMKilled with count 1 to meet threshold")
	}

	// Unhealthy with count 3 should NOT meet threshold (threshold is 5)
	event2 := newTestEventWithCount("default", "pod2", "Unhealthy", corev1.EventTypeWarning, 3)
	if f.MeetsThreshold(event2) {
		t.Error("expected Unhealthy with count 3 to NOT meet threshold of 5")
	}

	// Unhealthy with count 5 should meet threshold
	event3 := newTestEventWithCount("default", "pod3", "Unhealthy", corev1.EventTypeWarning, 5)
	if !f.MeetsThreshold(event3) {
		t.Error("expected Unhealthy with count 5 to meet threshold")
	}

	// Unhealthy with count 10 should meet threshold
	event4 := newTestEventWithCount("default", "pod4", "Unhealthy", corev1.EventTypeWarning, 10)
	if !f.MeetsThreshold(event4) {
		t.Error("expected Unhealthy with count 10 to meet threshold")
	}
}

func TestFilter_MeetsThreshold_NoThresholdConfigured(t *testing.T) {
	// Empty thresholds map - all events should pass
	f := New(nil, nil, []string{"SomeReason"}, map[string]int32{})

	event := newTestEventWithCount("default", "pod1", "SomeReason", corev1.EventTypeWarning, 1)
	if !f.MeetsThreshold(event) {
		t.Error("expected event with no threshold configured to pass")
	}
}

func TestFilter_GetThreshold(t *testing.T) {
	thresholds := map[string]int32{
		"OOMKilled": 1,
		"Unhealthy": 5,
	}
	f := New(nil, nil, []string{}, thresholds)

	if f.GetThreshold("OOMKilled") != 1 {
		t.Errorf("expected OOMKilled threshold 1, got %d", f.GetThreshold("OOMKilled"))
	}

	if f.GetThreshold("Unhealthy") != 5 {
		t.Errorf("expected Unhealthy threshold 5, got %d", f.GetThreshold("Unhealthy"))
	}

	// Unknown reason should return default of 1
	if f.GetThreshold("Unknown") != 1 {
		t.Errorf("expected Unknown threshold 1 (default), got %d", f.GetThreshold("Unknown"))
	}
}
