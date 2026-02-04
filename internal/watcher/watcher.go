package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/imankulov/kube-sentry-events/internal/dedup"
	"github.com/imankulov/kube-sentry-events/internal/filter"
	"github.com/imankulov/kube-sentry-events/internal/sentry"
)

// EventSender is the interface for sending events (Sentry or dry-run).
type EventSender interface {
	Send(data sentry.EventData)
}

// Watcher watches Kubernetes events and sends them to Sentry.
type Watcher struct {
	client kubernetes.Interface
	filter *filter.Filter
	dedup  *dedup.Deduplicator
	sender EventSender
	logger *slog.Logger
}

// New creates a new event watcher.
func New(f *filter.Filter, d *dedup.Deduplicator, s EventSender, logger *slog.Logger, kubeconfigPath string) (*Watcher, error) {
	client, err := createK8sClient(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return &Watcher{
		client: client,
		filter: f,
		dedup:  d,
		sender: s,
		logger: logger,
	}, nil
}

// Run starts watching for events. It blocks until the context is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	w.logger.Info("starting event watcher")

	for {
		if err := w.watchEvents(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.logger.Error("watch error, reconnecting", "error", err)
			time.Sleep(5 * time.Second)
		}
	}
}

// ListOnce lists all current events that match the filter and exits.
func (w *Watcher) ListOnce(ctx context.Context) error {
	w.logger.Info("listing current events (once mode)")

	events, err := w.client.CoreV1().Events("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list events: %w", err)
	}

	w.logger.Info("found events", "total", len(events.Items))

	matched := 0
	for i := range events.Items {
		event := &events.Items[i]
		if w.filter.ShouldProcess(event) {
			matched++
			w.processEvent(event)
		}
	}

	w.logger.Info("processed matching events", "matched", matched, "total", len(events.Items))
	return nil
}

func (w *Watcher) watchEvents(ctx context.Context) error {
	// Watch events across all namespaces
	watcher, err := w.client.CoreV1().Events("").Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to create event watch: %w", err)
	}
	defer watcher.Stop()

	w.logger.Info("watching for kubernetes events")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed")
			}

			if event.Type == watch.Error {
				return fmt.Errorf("watch error: %v", event.Object)
			}

			if event.Type != watch.Added && event.Type != watch.Modified {
				continue
			}

			k8sEvent, ok := event.Object.(*corev1.Event)
			if !ok {
				continue
			}

			w.processEvent(k8sEvent)
		}
	}
}

func (w *Watcher) processEvent(event *corev1.Event) {
	// Apply filter (namespace, event type, reason)
	if !w.filter.ShouldProcess(event) {
		return
	}

	namespace := event.InvolvedObject.Namespace
	if namespace == "" {
		namespace = event.Namespace
	}
	podName := event.InvolvedObject.Name
	reason := event.Reason

	// Extract deployment name for dedup - this groups events across pod rollouts
	// e.g., "worker-79c6dd4b57-wcdzt" -> "worker"
	deployment := sentry.ExtractDeploymentName(podName)

	// Get severity
	severity := w.filter.GetSeverity(reason)

	// Check if event meets threshold for creating an Issue
	meetsThreshold := w.filter.MeetsThreshold(event)

	// Check deduplication by deployment (not pod) - only applies to Issues, not Logs
	// This aligns with Sentry fingerprinting and reduces noise across rollouts
	isNew, count, firstSeen, lastSeen := w.dedup.Check(namespace, deployment, reason)
	shouldCreateIssue := meetsThreshold && isNew

	if !isNew && meetsThreshold {
		w.logger.Debug("skipping duplicate issue (log still sent)",
			"namespace", namespace,
			"deployment", deployment,
			"pod", podName,
			"reason", reason,
			"count", count,
		)
	}

	if shouldCreateIssue {
		w.logger.Info("sending event to sentry (log + issue)",
			"namespace", namespace,
			"deployment", deployment,
			"pod", podName,
			"reason", reason,
			"severity", severity,
			"k8s_count", event.Count,
		)
	} else {
		w.logger.Debug("sending event to sentry (log only)",
			"namespace", namespace,
			"deployment", deployment,
			"pod", podName,
			"reason", reason,
			"k8s_count", event.Count,
			"threshold", w.filter.GetThreshold(reason),
		)
	}

	// Send to Sentry - logs for ALL events, issues only if meets threshold AND not deduped
	w.sender.Send(sentry.EventData{
		Event:          event,
		Severity:       severity,
		Count:          count,
		FirstSeen:      firstSeen,
		LastSeen:       lastSeen,
		MeetsThreshold: shouldCreateIssue,
	})
}

func createK8sClient(kubeconfigPath string) (kubernetes.Interface, error) {
	var config *rest.Config
	var err error

	if kubeconfigPath != "" {
		// Use explicit kubeconfig path
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", kubeconfigPath, err)
		}
	} else {
		// Try in-cluster config first
		config, err = rest.InClusterConfig()
		if err != nil {
			// Fall back to kubeconfig for local development
			config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
			if err != nil {
				return nil, fmt.Errorf("failed to create config: %w", err)
			}
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return client, nil
}
