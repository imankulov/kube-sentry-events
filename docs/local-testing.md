# Local Testing Guide

This guide explains how to test `kube-sentry-events` locally before deploying to a Kubernetes cluster.

## Prerequisites

- Go 1.25+
- Access to a Kubernetes cluster (via `kubectl`)
- (Optional) A Sentry DSN for end-to-end testing

## Build

```bash
go build -o kube-sentry-events ./cmd/kube-sentry-events
```

## CLI Flags

| Flag           | Description                                            |
| -------------- | ------------------------------------------------------ |
| `--dry-run`    | Print events to stdout instead of sending to Sentry    |
| `--kubeconfig` | Path to kubeconfig file (defaults to `~/.kube/config`) |
| `--once`       | List matching events once and exit (don't watch)       |
| `--version`    | Show version and exit                                  |

## Testing Scenarios

### 1. Dry-Run Mode (Recommended for Initial Testing)

View events without sending to Sentry:

```bash
# List all current matching events and exit
./kube-sentry-events --dry-run --once

# Watch for new events continuously
./kube-sentry-events --dry-run
```

**Sample output:**

```json
{
  "message": "OOMKilled: worker-79c6dd4b57-wcdzt",
  "severity": "error",
  "meets_threshold": true,
  "mode": "log + issue",
  "tags": {
    "k8s.namespace": "production",
    "k8s.pod": "worker-79c6dd4b57-wcdzt",
    "k8s.reason": "OOMKilled",
    "k8s.kind": "Pod",
    "k8s.node": "node-1",
    "k8s.deployment": "worker"
  },
  "extra": {
    "message": "Container worker was OOMKilled",
    "count": 1,
    "k8s_event_count": 1,
    "first_seen": "2026-02-04T12:00:00Z",
    "last_seen": "2026-02-04T12:00:00Z"
  },
  "fingerprint": ["k8s", "production", "worker", "OOMKilled"]
}
```

**Note:** `mode` shows how the event would be handled:

- `"log + issue"`: Sent to both Sentry Logs and creates a Sentry Issue (meets threshold)
- `"log only"`: Sent only to Sentry Logs (below threshold, e.g., transient probe failures)

### 2. Test with Specific Kubeconfig

Use a specific cluster context:

```bash
# Use a different kubeconfig file
./kube-sentry-events --dry-run --kubeconfig=/path/to/kubeconfig

# Or set the KUBECONFIG environment variable
KUBECONFIG=/path/to/kubeconfig ./kube-sentry-events --dry-run
```

### 3. Filter by Namespace

Test with specific namespaces:

```bash
# Watch only 'staging' namespace
KUBE_SENTRY_NAMESPACES=staging ./kube-sentry-events --dry-run

# Exclude multiple namespaces
KUBE_SENTRY_EXCLUDE_NAMESPACES="kube-system,monitoring" ./kube-sentry-events --dry-run
```

### 4. Filter by Event Type

Test with specific event reasons:

```bash
# Watch only OOMKilled and CrashLoopBackOff
KUBE_SENTRY_EVENTS="OOMKilled,CrashLoopBackOff" ./kube-sentry-events --dry-run --once
```

### 5. Test Threshold Behavior

See how thresholds affect which events create Issues:

```bash
# With default thresholds (Unhealthy requires count >= 5)
./kube-sentry-events --dry-run --once

# Lower the threshold for Unhealthy events
KUBE_SENTRY_THRESHOLDS="Unhealthy:2" ./kube-sentry-events --dry-run --once

# Require more occurrences before alerting on BackOff
KUBE_SENTRY_THRESHOLDS="BackOff:10,Unhealthy:10" ./kube-sentry-events --dry-run --once
```

### 6. End-to-End Test with Sentry

Send events to Sentry from your local machine:

```bash
# Full integration test (with Sentry Logs enabled by default)
SENTRY_DSN="https://xxx@xxx.ingest.sentry.io/xxx" \
SENTRY_ENVIRONMENT="local-testing" \
./kube-sentry-events --once

# Disable Sentry Logs (only create Issues)
SENTRY_DSN="https://xxx@xxx.ingest.sentry.io/xxx" \
SENTRY_ENVIRONMENT="local-testing" \
KUBE_SENTRY_ENABLE_LOGS="false" \
./kube-sentry-events --once
```

### 7. Debug Mode

Enable verbose logging:

```bash
KUBE_SENTRY_LOG_LEVEL=debug ./kube-sentry-events --dry-run
```

## Generating Test Events

To test the tool, you can generate Kubernetes events in your cluster.

### Quick Test Script

```bash
#!/bin/bash
# Create all test pods at once
kubectl run crash-test --image=busybox --restart=Always -- /bin/false
kubectl run pull-test --image=nonexistent/image:v999 --restart=Never
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: oom-test
spec:
  restartPolicy: Never
  containers:
  - name: stress
    image: polinux/stress
    resources:
      limits:
        memory: "50Mi"
    command: ["stress", "--vm", "1", "--vm-bytes", "100M", "--timeout", "60s"]
EOF

# Wait for events
sleep 20

# Run kube-sentry-events
./kube-sentry-events --dry-run --once

# Cleanup
kubectl delete pod crash-test pull-test oom-test --ignore-not-found
```

### Individual Test Scenarios

#### BackOff / CrashLoopBackOff

Container exits immediately and keeps restarting:

```bash
kubectl run crash-test --image=busybox --restart=Always -- /bin/false

# Wait ~30 seconds for BackOff events
kubectl get events --field-selector involvedObject.name=crash-test -w

# Cleanup
kubectl delete pod crash-test
```

**Expected events:** `BackOff` (threshold: 3), eventually `CrashLoopBackOff` (threshold: 1)

#### ImagePullBackOff / ErrImagePull

Non-existent container image:

```bash
kubectl run pull-test --image=nonexistent/image:v999 --restart=Never

# Watch events
kubectl get events --field-selector involvedObject.name=pull-test -w

# Cleanup
kubectl delete pod pull-test
```

**Expected events:** `ErrImagePull` (threshold: 2), `ImagePullBackOff` (threshold: 3)

#### OOMKilled

Container exceeds memory limit:

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: oom-test
spec:
  restartPolicy: Never
  containers:
  - name: stress
    image: polinux/stress
    resources:
      limits:
        memory: "50Mi"
    command: ["stress", "--vm", "1", "--vm-bytes", "100M", "--timeout", "60s"]
EOF

# Watch for OOMKilled (may take 10-30 seconds)
kubectl get events --field-selector involvedObject.name=oom-test -w

# Cleanup
kubectl delete pod oom-test
```

**Expected events:** `OOMKilled` (threshold: 1 - immediate alert)

#### Unhealthy (Probe Failures)

Failing liveness probe:

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: unhealthy-test
spec:
  restartPolicy: Never
  containers:
  - name: nginx
    image: nginx
    livenessProbe:
      httpGet:
        path: /nonexistent
        port: 80
      initialDelaySeconds: 5
      periodSeconds: 5
      failureThreshold: 1
EOF

# Watch for Unhealthy events
kubectl get events --field-selector involvedObject.name=unhealthy-test -w

# Cleanup
kubectl delete pod unhealthy-test
```

**Expected events:** `Unhealthy` (threshold: 5 - needs multiple failures before alert)

#### FailedMount

Missing ConfigMap/Secret reference:

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: mount-test
spec:
  restartPolicy: Never
  containers:
  - name: nginx
    image: nginx
    volumeMounts:
    - name: config
      mountPath: /etc/config
  volumes:
  - name: config
    configMap:
      name: nonexistent-config
EOF

# Watch for FailedMount events
kubectl get events --field-selector involvedObject.name=mount-test -w

# Cleanup
kubectl delete pod mount-test
```

**Expected events:** `FailedMount` (threshold: 1 - immediate alert)

### Cleanup All Test Pods

```bash
kubectl delete pod crash-test pull-test oom-test unhealthy-test mount-test --ignore-not-found
```

## Troubleshooting

### "failed to create Kubernetes client"

- Ensure `kubectl` can connect to your cluster: `kubectl cluster-info`
- Check your kubeconfig: `kubectl config view`
- Verify the kubeconfig path with `--kubeconfig`

### No Events Showing

- Check if there are any Warning events: `kubectl get events --field-selector type=Warning`
- Verify namespace filters aren't excluding your events
- Use `--once` to see all current matching events

### Events Not Appearing in Sentry

- Verify the DSN is correct
- Check the Sentry environment filter in your Sentry project settings
- Look for network errors in debug logs (`KUBE_SENTRY_LOG_LEVEL=debug`)

## Environment Variables Reference

| Variable                         | Default        | Description                                                         |
| -------------------------------- | -------------- | ------------------------------------------------------------------- |
| `SENTRY_DSN`                     | (required\*)   | Sentry DSN (\*not required in `--dry-run` mode)                     |
| `SENTRY_ENVIRONMENT`             | `production`   | Sentry environment tag                                              |
| `KUBE_SENTRY_NAMESPACES`         | (all)          | Comma-separated namespaces to watch                                 |
| `KUBE_SENTRY_EXCLUDE_NAMESPACES` | `kube-system`  | Namespaces to exclude                                               |
| `KUBE_SENTRY_EVENTS`             | (all critical) | Event reasons to monitor                                            |
| `KUBE_SENTRY_THRESHOLDS`         | (see README)   | Min event count before creating Issues (format: `Reason:count,...`) |
| `KUBE_SENTRY_ENABLE_LOGS`        | `true`         | Send all events to Sentry Logs for observability                    |
| `KUBE_SENTRY_DEDUP_WINDOW`       | `5m`           | Deduplication time window                                           |
| `KUBE_SENTRY_LOG_LEVEL`          | `info`         | Log level (debug, info, warn, error)                                |
