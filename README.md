# kube-sentry-events

A lightweight Kubernetes event watcher that sends critical cluster events to Sentry.

## Why?

When Kubernetes pods are OOMKilled, crash, or experience other critical failures, application-level error tracking doesn't capture these events because:

- **OOMKilled** processes are terminated instantly by the kernel - no chance to report
- **ImagePull failures** happen before the application starts
- **Scheduling failures** occur at the cluster level, not application level

## Features

- 🎯 **Focused** - Monitors only critical Kubernetes events
- 🪶 **Lightweight** - Single binary, <64Mi memory, no external dependencies
- 🔄 **Deduplication** - Prevents duplicate Sentry issues for repeated events
- 📊 **Smart grouping** - Events grouped by deployment+reason in Sentry
- ⚙️ **Configurable** - Filter by namespace, event type, and more
- 📈 **Dual-mode** - Sentry Logs for observability + Sentry Issues for critical alerts
- 🎚️ **Thresholds** - Filter transient events (e.g., require 5 probe failures before alerting)
- 🔧 **Troubleshooting** - Issues include likely causes, debug commands, and runbook links

## Quick Start

```bash
# Using Helm
helm install kube-sentry-events ./deploy/helm/kube-sentry-events \
  --set sentry.dsn="https://xxx@xxx.ingest.sentry.io/xxx"

# Or using kubectl
kubectl apply -f deploy/kubernetes/
```

## Monitored Events

| Event              | Description                          | Severity |
| ------------------ | ------------------------------------ | -------- |
| `OOMKilled`        | Container killed due to memory limit | error    |
| `CrashLoopBackOff` | Container repeatedly crashing        | error    |
| `FailedScheduling` | Pod cannot be scheduled              | error    |
| `Evicted`          | Pod evicted due to node pressure     | error    |
| `ImagePullBackOff` | Cannot pull container image          | error    |
| `FailedMount`      | Volume mount failed                  | error    |
| `Unhealthy`        | Liveness/readiness probe failed      | warning  |

## Dual-Mode: Logs + Issues

kube-sentry-events supports two complementary modes:

1. **Sentry Logs** (observability) - ALL matching events are sent to [Sentry Logs](https://docs.sentry.io/product/explore/logs/) for searchability and dashboards
2. **Sentry Issues** (alerting) - Only events meeting thresholds create Issues for alerting

This allows you to:

- Search and analyze all k8s events in Sentry Logs
- Only get alerted on persistent/critical issues
- Filter out transient events (like temporary probe failures during deployments)

## Event Thresholds

Thresholds define the minimum k8s event count before creating a Sentry Issue:

| Event              | Default Threshold | Rationale                     |
| ------------------ | ----------------- | ----------------------------- |
| `OOMKilled`        | 1                 | Always critical               |
| `CrashLoopBackOff` | 1                 | Always critical               |
| `FailedScheduling` | 1                 | Always critical               |
| `Evicted`          | 1                 | Always critical               |
| `FailedMount`      | 1                 | Always critical               |
| `Unhealthy`        | 5                 | Common during rolling updates |
| `BackOff`          | 3                 | May be temporary              |
| `ImagePullBackOff` | 3                 | May be registry issues        |

Override thresholds via `KUBE_SENTRY_THRESHOLDS=Unhealthy:10,BackOff:5`.

## Configuration

| Environment Variable             | Default        | Description                                    |
| -------------------------------- | -------------- | ---------------------------------------------- |
| `SENTRY_DSN`                     | (required)     | Sentry DSN                                     |
| `SENTRY_ENVIRONMENT`             | `production`   | Sentry environment tag                         |
| `KUBE_SENTRY_NAMESPACES`         | (all)          | Comma-separated namespaces to watch            |
| `KUBE_SENTRY_EXCLUDE_NAMESPACES` | `kube-system`  | Namespaces to exclude                          |
| `KUBE_SENTRY_EVENTS`             | (all critical) | Event reasons to monitor                       |
| `KUBE_SENTRY_THRESHOLDS`         | (see above)    | Custom thresholds (format: `Reason:count,...`) |
| `KUBE_SENTRY_ENABLE_LOGS`        | `true`         | Send all events to Sentry Logs                 |
| `KUBE_SENTRY_DEDUP_WINDOW`       | `5m`           | Deduplication time window                      |
| `KUBE_SENTRY_LOG_LEVEL`          | `info`         | Log level (debug, info, warn, error)           |

## Sentry Event Structure

### Sentry Issues (critical events)

Issues include:

- **Tags**: `k8s.namespace`, `k8s.pod`, `k8s.node`, `k8s.reason`, `k8s.deployment`
- **Fingerprint**: Groups by `[namespace, deployment, reason]` for smart issue grouping
- **Extra data**: Event message, count, first/last seen timestamps
- **Troubleshooting context**:
  - `description`: What the event means
  - `likely_causes`: Common root causes
  - `debug_commands`: kubectl commands to investigate
  - `runbook_url`: Link to Kubernetes documentation
- **Breadcrumbs**: Pre-populated kubectl commands for quick debugging

### Sentry Logs (all events)

Logs include attributes for filtering:

- `k8s.namespace`, `k8s.pod`, `k8s.node`, `k8s.reason`, `k8s.kind`, `k8s.deployment`
- `k8s.event_count`: Number of times this event occurred

## Development

```bash
# Build
go build -o kube-sentry-events ./cmd/kube-sentry-events

# Test
go test ./...

# Run locally (requires kubeconfig)
SENTRY_DSN=https://xxx@xxx.ingest.sentry.io/xxx ./kube-sentry-events
```

## License

MIT License - see [LICENSE](LICENSE)
