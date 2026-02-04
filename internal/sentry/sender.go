package sentry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	corev1 "k8s.io/api/core/v1"
)

// EventData contains processed event information for Sentry.
type EventData struct {
	Event          *corev1.Event
	Severity       sentry.Level
	Count          int
	FirstSeen      time.Time
	LastSeen       time.Time
	MeetsThreshold bool // Whether this event should create an Issue
}

// Sender sends Kubernetes events to Sentry.
type Sender struct {
	environment string
	enableLogs  bool
	logger      sentry.Logger
}

// New creates a new Sentry sender.
func New(dsn, environment string, enableLogs bool) (*Sender, error) {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      environment,
		EnableLogs:       enableLogs,
		AttachStacktrace: false,
		// Release can be set via SENTRY_RELEASE env var
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Sentry: %w", err)
	}

	var logger sentry.Logger
	if enableLogs {
		logger = sentry.NewLogger(context.Background())
	}

	return &Sender{
		environment: environment,
		enableLogs:  enableLogs,
		logger:      logger,
	}, nil
}

// Send sends a Kubernetes event to Sentry.
// If enableLogs is true, ALL events are sent to Sentry Logs.
// If MeetsThreshold is true, the event also creates a Sentry Issue.
func (s *Sender) Send(data EventData) {
	event := data.Event

	// Extract metadata
	namespace := event.InvolvedObject.Namespace
	if namespace == "" {
		namespace = event.Namespace
	}
	podName := event.InvolvedObject.Name
	nodeName := event.Source.Host
	reason := event.Reason
	kind := event.InvolvedObject.Kind
	deployment := ExtractDeploymentName(podName)

	// Always send to Sentry Logs if enabled (for observability)
	if s.enableLogs {
		s.sendLog(data, namespace, podName, nodeName, reason, kind, deployment)
	}

	// Only create Issue if event meets threshold (for alerting)
	if data.MeetsThreshold {
		s.sendIssue(data, namespace, podName, nodeName, reason, kind, deployment)
	}
}

// sendLog sends the event to Sentry Logs for observability.
func (s *Sender) sendLog(data EventData, namespace, podName, nodeName, reason, kind, deployment string) {
	event := data.Event

	// Map Sentry Level to Log Level
	var logEntry sentry.LogEntry
	switch data.Severity {
	case sentry.LevelError, sentry.LevelFatal:
		logEntry = s.logger.Error()
	case sentry.LevelWarning:
		logEntry = s.logger.Warn()
	default:
		logEntry = s.logger.Info()
	}

	// Add attributes for searchability
	logEntry = logEntry.
		String("k8s.namespace", namespace).
		String("k8s.pod", podName).
		String("k8s.reason", reason).
		String("k8s.kind", kind).
		String("k8s.deployment", deployment).
		Int("k8s.event_count", int(event.Count))

	if nodeName != "" {
		logEntry = logEntry.String("k8s.node", nodeName)
	}

	// Emit the log
	logEntry.Emitf("[%s] %s: %s - %s", namespace, reason, podName, event.Message)
}

// sendIssue creates a Sentry Issue for critical events.
func (s *Sender) sendIssue(data EventData, namespace, podName, nodeName, reason, kind, deployment string) {
	event := data.Event

	// Build message
	message := fmt.Sprintf("%s: %s", reason, podName)

	// Get troubleshooting context
	troubleshooting := getTroubleshootingContext(reason)

	// Create Sentry event
	sentryEvent := &sentry.Event{
		Message: message,
		Level:   data.Severity,
		Tags: map[string]string{
			"k8s.namespace": namespace,
			"k8s.pod":       podName,
			"k8s.reason":    reason,
			"k8s.kind":      kind,
		},
		Extra: map[string]interface{}{
			"message":    event.Message,
			"count":      data.Count,
			"first_seen": data.FirstSeen.UTC().Format(time.RFC3339),
			"last_seen":  data.LastSeen.UTC().Format(time.RFC3339),
			// Troubleshooting guidance
			"description":    troubleshooting.Description,
			"likely_causes":  troubleshooting.LikelyCauses,
			"debug_commands": troubleshooting.DebugCommands,
			"runbook_url":    troubleshooting.RunbookURL,
		},
		// Fingerprint groups related events together
		Fingerprint: []string{"k8s", namespace, deployment, reason},
	}

	// Add optional tags
	if nodeName != "" {
		sentryEvent.Tags["k8s.node"] = nodeName
	}
	if deployment != "" && deployment != podName {
		sentryEvent.Tags["k8s.deployment"] = deployment
	}

	// Add event timestamps
	if !event.FirstTimestamp.IsZero() {
		sentryEvent.Extra["k8s_first_timestamp"] = event.FirstTimestamp.UTC().Format(time.RFC3339)
	}
	if !event.LastTimestamp.IsZero() {
		sentryEvent.Extra["k8s_last_timestamp"] = event.LastTimestamp.UTC().Format(time.RFC3339)
	}
	if event.Count > 0 {
		sentryEvent.Extra["k8s_event_count"] = event.Count
	}

	// Add breadcrumbs with kubectl commands for debugging
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: "debug",
		Message:  fmt.Sprintf("kubectl describe pod %s -n %s", podName, namespace),
		Level:    sentry.LevelInfo,
	})
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: "debug",
		Message:  fmt.Sprintf("kubectl logs %s -n %s --previous", podName, namespace),
		Level:    sentry.LevelInfo,
	})
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: "debug",
		Message:  fmt.Sprintf("kubectl get events -n %s --field-selector involvedObject.name=%s", namespace, podName),
		Level:    sentry.LevelInfo,
	})

	sentry.CaptureEvent(sentryEvent)
}

// Flush waits for all events to be sent.
func (s *Sender) Flush(timeout time.Duration) bool {
	return sentry.Flush(timeout)
}

// DryRunSender prints events to an io.Writer instead of sending to Sentry.
type DryRunSender struct {
	writer io.Writer
}

// NewDryRunSender creates a sender that outputs to the given writer.
func NewDryRunSender(w io.Writer) *DryRunSender {
	return &DryRunSender{writer: w}
}

// Send prints the event data as JSON to the writer.
func (d *DryRunSender) Send(data EventData) {
	event := data.Event

	namespace := event.InvolvedObject.Namespace
	if namespace == "" {
		namespace = event.Namespace
	}

	output := map[string]interface{}{
		"message":         fmt.Sprintf("%s: %s", event.Reason, event.InvolvedObject.Name),
		"severity":        string(data.Severity),
		"meets_threshold": data.MeetsThreshold,
		"mode":            getModeString(data.MeetsThreshold),
		"tags": map[string]string{
			"k8s.namespace":  namespace,
			"k8s.pod":        event.InvolvedObject.Name,
			"k8s.reason":     event.Reason,
			"k8s.kind":       event.InvolvedObject.Kind,
			"k8s.node":       event.Source.Host,
			"k8s.deployment": ExtractDeploymentName(event.InvolvedObject.Name),
		},
		"extra": map[string]interface{}{
			"message":         event.Message,
			"count":           data.Count,
			"k8s_event_count": event.Count,
			"first_seen":      data.FirstSeen.UTC().Format(time.RFC3339),
			"last_seen":       data.LastSeen.UTC().Format(time.RFC3339),
		},
		"fingerprint": []string{"k8s", namespace, ExtractDeploymentName(event.InvolvedObject.Name), event.Reason},
	}

	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		_, _ = fmt.Fprintf(d.writer, "ERROR: failed to marshal event: %v\n", err)
		return
	}
	_, _ = fmt.Fprintf(d.writer, "%s\n", jsonData)
}

func getModeString(meetsThreshold bool) string {
	if meetsThreshold {
		return "log + issue"
	}
	return "log only"
}

// Flush is a no-op for dry-run sender.
func (d *DryRunSender) Flush(_ time.Duration) bool {
	return true
}

// TroubleshootingContext provides guidance for debugging k8s events.
type TroubleshootingContext struct {
	Description   string
	LikelyCauses  []string
	DebugCommands []string
	RunbookURL    string
}

func getTroubleshootingContext(reason string) TroubleshootingContext {
	contexts := map[string]TroubleshootingContext{
		"OOMKilled": {
			Description: "Container was terminated because it exceeded its memory limit.",
			LikelyCauses: []string{
				"Memory limit set too low for the workload",
				"Memory leak in the application",
				"Spike in traffic causing increased memory usage",
				"Large data processing without streaming",
			},
			DebugCommands: []string{
				"kubectl top pod <pod> -n <namespace>",
				"kubectl describe pod <pod> -n <namespace> | grep -A5 'Last State'",
				"kubectl logs <pod> -n <namespace> --previous",
			},
			RunbookURL: "https://kubernetes.io/docs/tasks/debug/debug-application/debug-running-pod/#container-is-terminated",
		},
		"CrashLoopBackOff": {
			Description: "Container keeps crashing and Kubernetes is backing off from restarting it.",
			LikelyCauses: []string{
				"Application crashes on startup (check logs)",
				"Missing configuration or secrets",
				"Liveness probe failing",
				"Dependency not available (database, external service)",
			},
			DebugCommands: []string{
				"kubectl logs <pod> -n <namespace> --previous",
				"kubectl describe pod <pod> -n <namespace>",
				"kubectl get events -n <namespace> --field-selector involvedObject.name=<pod>",
			},
			RunbookURL: "https://kubernetes.io/docs/tasks/debug/debug-application/debug-running-pod/",
		},
		"ImagePullBackOff": {
			Description: "Kubernetes cannot pull the container image.",
			LikelyCauses: []string{
				"Image tag doesn't exist",
				"Private registry authentication failed",
				"Registry is unreachable",
				"Image name is misspelled",
			},
			DebugCommands: []string{
				"kubectl describe pod <pod> -n <namespace> | grep -A10 Events",
				"kubectl get secret -n <namespace>",
				"docker pull <image> (test locally)",
			},
			RunbookURL: "https://kubernetes.io/docs/concepts/containers/images/#image-pull-policy",
		},
		"Unhealthy": {
			Description: "Container failed its liveness or readiness probe.",
			LikelyCauses: []string{
				"Application is slow to start (increase initialDelaySeconds)",
				"Health endpoint is misconfigured",
				"Application is overloaded",
				"Dependency timeout affecting health check",
			},
			DebugCommands: []string{
				"kubectl describe pod <pod> -n <namespace> | grep -A20 'Liveness\\|Readiness'",
				"kubectl logs <pod> -n <namespace> --tail=100",
				"kubectl exec <pod> -n <namespace> -- curl -v localhost:<port>/<health-path>",
			},
			RunbookURL: "https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/",
		},
		"Evicted": {
			Description: "Pod was evicted from the node, usually due to resource pressure.",
			LikelyCauses: []string{
				"Node is running out of disk space",
				"Node is running out of memory",
				"Too many pods on the node",
				"Pod exceeded ephemeral storage limit",
			},
			DebugCommands: []string{
				"kubectl describe node <node>",
				"kubectl get pods -A -o wide --field-selector spec.nodeName=<node>",
				"kubectl top node <node>",
			},
			RunbookURL: "https://kubernetes.io/docs/concepts/scheduling-eviction/node-pressure-eviction/",
		},
		"FailedScheduling": {
			Description: "Kubernetes cannot find a node to schedule the pod.",
			LikelyCauses: []string{
				"Insufficient CPU or memory in cluster",
				"Node selector/affinity doesn't match any nodes",
				"Taints preventing scheduling",
				"PersistentVolumeClaim not bound",
			},
			DebugCommands: []string{
				"kubectl describe pod <pod> -n <namespace> | grep -A10 Events",
				"kubectl get nodes -o wide",
				"kubectl describe nodes | grep -A5 'Allocated resources'",
			},
			RunbookURL: "https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/",
		},
		"FailedMount": {
			Description: "Volume could not be mounted to the pod.",
			LikelyCauses: []string{
				"PersistentVolume not available",
				"Secret or ConfigMap doesn't exist",
				"NFS/cloud storage connectivity issue",
				"Volume is already mounted elsewhere (ReadWriteOnce)",
			},
			DebugCommands: []string{
				"kubectl describe pod <pod> -n <namespace>",
				"kubectl get pv,pvc -n <namespace>",
				"kubectl get events -n <namespace> | grep -i mount",
			},
			RunbookURL: "https://kubernetes.io/docs/concepts/storage/persistent-volumes/",
		},
		"BackOff": {
			Description: "Container is in back-off state, waiting before restart.",
			LikelyCauses: []string{
				"Previous container crash (check logs)",
				"Exit code non-zero",
				"Repeated failures triggering exponential backoff",
			},
			DebugCommands: []string{
				"kubectl logs <pod> -n <namespace> --previous",
				"kubectl describe pod <pod> -n <namespace>",
			},
			RunbookURL: "https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#restart-policy",
		},
	}

	if ctx, ok := contexts[reason]; ok {
		return ctx
	}

	return TroubleshootingContext{
		Description:  fmt.Sprintf("Kubernetes event: %s", reason),
		LikelyCauses: []string{"Check pod events and logs for details"},
		DebugCommands: []string{
			"kubectl describe pod <pod> -n <namespace>",
			"kubectl logs <pod> -n <namespace>",
		},
		RunbookURL: "https://kubernetes.io/docs/tasks/debug/",
	}
}

// ExtractDeploymentName attempts to extract the deployment name from a pod name.
// Kubernetes pod names typically follow the pattern: deployment-replicaset-pod
// e.g., "worker-79c6dd4b57-wcdzt" -> "worker"
func ExtractDeploymentName(podName string) string {
	parts := strings.Split(podName, "-")
	if len(parts) < 3 {
		return podName
	}

	// Check if last two parts look like replicaset hash and pod hash
	// ReplicaSet hash is typically 9-10 alphanumeric chars
	// Pod hash is typically 5 alphanumeric chars
	lastPart := parts[len(parts)-1]
	secondLastPart := parts[len(parts)-2]

	// If the last part is short (pod hash) and second-to-last is longer (replicaset hash)
	if len(lastPart) <= 6 && len(secondLastPart) >= 5 && len(secondLastPart) <= 12 {
		if isAlphanumeric(lastPart) && isAlphanumeric(secondLastPart) {
			return strings.Join(parts[:len(parts)-2], "-")
		}
	}

	return podName
}

func isAlphanumeric(s string) bool {
	for _, r := range s {
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		if !isLower && !isDigit {
			return false
		}
	}
	return true
}

// Ensure attribute package is used
var _ = attribute.String
