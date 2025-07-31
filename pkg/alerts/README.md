# Kubernetes Pod Alerts Tool

This tool provides comprehensive monitoring and analysis of Kubernetes pod alerts with AI-powered insights.

## Features

### 1. Pod Alert Detection
- Automatically identifies problematic pods across namespaces
- Detects various failure states: Pending, Failed, CrashLoopBackOff, ImagePullBackOff, etc.
- Analyzes container statuses and pod conditions

### 2. Detailed Analysis
- Collects pod events and logs for context
- Provides root cause analysis using AI
- Suggests remediation steps and prevention strategies

### 3. Cluster-wide Monitoring
- Scans entire cluster for pod issues
- Identifies common patterns and cluster-wide problems
- Provides strategic recommendations for cluster health

## Available Tools

### `alerts_get_pod_alerts`
Get all pod alerts in a namespace or cluster.

**Parameters:**
- `namespace` (optional): Specific namespace to check
- `all_namespaces` (optional): Check all namespaces (true/false)
- `include_analysis` (optional): Include AI analysis of alerts (true/false)

**Example:**
```json
{
  "tool": "alerts_get_pod_alerts",
  "arguments": {
    "namespace": "default",
    "include_analysis": "true"
  }
}
```

### `alerts_get_pod_alert_details`
Get detailed information about a specific pod alert.

**Parameters:**
- `pod_name` (required): Name of the pod
- `namespace` (optional): Namespace of the pod (default: default)
- `include_analysis` (optional): Include AI analysis (true/false)

**Example:**
```json
{
  "tool": "alerts_get_pod_alert_details",
  "arguments": {
    "pod_name": "my-app-pod",
    "namespace": "production",
    "include_analysis": "true"
  }
}
```

### `alerts_get_cluster_alerts`
Get all alerts across the entire cluster.

**Parameters:**
- `include_analysis` (optional): Include AI analysis of cluster alerts (true/false)

**Example:**
```json
{
  "tool": "alerts_get_cluster_alerts",
  "arguments": {
    "include_analysis": "true"
  }
}
```

## Alert Types Detected

1. **Pod Status Issues:**
   - Pending
   - Failed
   - Unknown
   - CrashLoopBackOff
   - ImagePullBackOff
   - ErrImagePull

2. **Container Issues:**
   - Containers not ready
   - Container restart loops
   - Image pull failures
   - Resource constraints

3. **Pod Condition Issues:**
   - Ready condition false
   - PodScheduled condition false
   - Initialized condition false

## AI Analysis Features

The tool uses AI to provide:

1. **Root Cause Analysis:** Identifies the underlying cause of pod failures
2. **Remediation Steps:** Provides specific actions to fix the issues
3. **Prevention Strategies:** Suggests ways to prevent similar issues
4. **Monitoring Recommendations:** Advises on better monitoring practices

## Usage Examples

### Basic Pod Alert Check
```bash
# Check for alerts in default namespace
curl -X POST http://localhost:8084/tools/alerts_get_pod_alerts \
  -H "Content-Type: application/json" \
  -d '{"namespace": "default"}'
```

### Detailed Pod Analysis
```bash
# Get detailed analysis of a specific pod
curl -X POST http://localhost:8084/tools/alerts_get_pod_alert_details \
  -H "Content-Type: application/json" \
  -d '{"pod_name": "my-app-pod", "include_analysis": "true"}'
```

### Cluster-wide Alert Scan
```bash
# Scan entire cluster for alerts
curl -X POST http://localhost:8084/tools/alerts_get_cluster_alerts \
  -H "Content-Type: application/json" \
  -d '{"include_analysis": "true"}'
```

## Response Format

The tool returns JSON responses with detailed alert information:

```json
[
  {
    "pod_name": "my-app-pod",
    "namespace": "default",
    "status": "CrashLoopBackOff",
    "reason": "CrashLoopBackOff",
    "message": "Back-off restarting failed container",
    "restart_count": 5,
    "events": [
      {
        "type": "Warning",
        "reason": "BackOff",
        "message": "Back-off restarting failed container",
        "count": 5,
        "first_time": "2024-01-01T10:00:00Z",
        "last_time": "2024-01-01T10:30:00Z"
      }
    ],
    "logs": [
      "Error: Cannot connect to database",
      "FATAL: connection to server failed"
    ],
    "analysis": "AI-generated analysis of the issue...",
    "remediation": "Suggested fixes..."
  }
]
```

## Future Enhancements

1. **Prometheus Integration:** Automatically trigger alerts based on Prometheus metrics
2. **Automated Remediation:** Execute fixes automatically when possible
3. **Alert History:** Track alert patterns over time
4. **Custom Alert Rules:** Allow users to define custom alert conditions
5. **Notification Integration:** Send alerts to Slack, email, etc.
6. **Dashboard Integration:** Provide visual dashboards for alert monitoring

## Dependencies

- Kubernetes cluster access
- kubectl configured
- Optional: LLM model for AI analysis (OpenAI, etc.)

## Security Considerations

- The tool only performs read operations on the cluster
- No automatic remediation without explicit user consent
- All kubectl commands are executed with proper error handling
- Sensitive information in logs is handled according to cluster policies 