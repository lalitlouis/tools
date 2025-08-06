# Alert Collector Tools

This package provides tools for collecting and analyzing Kubernetes alert data using LLM analysis.

## Overview

The Alert Collector tools enable comprehensive data collection and analysis for Kubernetes services experiencing issues. These tools can be used by kagent agents to:

1. **Collect Alert Data**: Gather comprehensive information about target services including pods, events, logs, and metrics
2. **Analyze Alert Data**: Perform LLM-based analysis to identify root causes and provide insights
3. **Generate Remediation Scripts**: Create actionable remediation scripts based on analysis
4. **Store Alert Data**: Persist collected data for historical analysis

## Available Tools

### `collect_alert_data`
Collects comprehensive data for a target service.

**Parameters:**
- `targetService` (required): Name of the target service to collect data for
- `namespace` (required): Namespace where the target service is running
- `collectPods` (optional): Whether to collect pod information (true/false)
- `collectEvents` (optional): Whether to collect events (true/false)
- `collectLogs` (optional): Whether to collect logs (true/false)
- `maxLogLines` (optional): Maximum number of log lines to collect

**Returns:** JSON object containing collected data

### `analyze_alert_data`
Performs LLM analysis on collected alert data.

**Parameters:**
- `data` (required): Collected alert data to analyze (JSON string)
- `promptTemplate` (optional): Custom prompt template for analysis

**Returns:** JSON object containing analysis results

### `generate_remediation_script`
Generates a remediation script based on alert analysis.

**Parameters:**
- `analysis` (required): Alert analysis result (JSON string)
- `alertType` (required): Type of alert (e.g., PodCrash, ServiceUnavailable)

**Returns:** Remediation script as text

### `store_alert_data`
Stores collected alert data to storage.

**Parameters:**
- `data` (required): Collected alert data to store (JSON string)
- `storagePath` (optional): Path where to store the data

**Returns:** JSON object with storage status

## Usage Example

```yaml
# Example Agent configuration using Alert Collector tools
apiVersion: kagent.dev/v1alpha1
kind: Agent
metadata:
  name: alert-collector-agent
  namespace: kagent
spec:
  name: "Alert Collector Agent"
  description: "An agent that collects and analyzes alert data using specialized tools"
  instruction: |
    You are an Alert Collector Agent responsible for monitoring and analyzing Kubernetes alerts.
    
    When an AlertCollector CR is created, you should:
    1. Use collect_alert_data tool to gather information about the target service
    2. Use analyze_alert_data tool to perform LLM analysis
    3. Use store_alert_data tool to persist the collected data
    4. Use generate_remediation_script tool to create actionable remediation steps
    
    Always provide clear, actionable insights and prioritize critical issues.
  
  modelConfig: "default-model-config"
  
  tools:
    - name: "collect_alert_data"
      description: "Collect comprehensive data for a target service including pods, events, logs, and metrics"
    - name: "analyze_alert_data"
      description: "Perform LLM analysis on collected alert data"
    - name: "store_alert_data"
      description: "Store collected alert data to PVC"
    - name: "generate_remediation_script"
      description: "Generate a remediation script based on alert analysis"
```

## Integration with AlertCollector CR

The AlertCollector Custom Resource can trigger these tools through the kagent framework:

1. **Detection**: AlertCollector CR detects an alert condition
2. **Collection**: Uses `collect_alert_data` to gather comprehensive data
3. **Analysis**: Uses `analyze_alert_data` to perform LLM analysis
4. **Storage**: Uses `store_alert_data` to persist data to PVC
5. **Remediation**: Uses `generate_remediation_script` to create actionable steps

## Data Collection

The tools collect the following types of data:

- **Pod Information**: Status, conditions, restart counts, labels
- **Events**: Kubernetes events related to the target service
- **Logs**: Container logs from pods in the target service
- **Metrics**: Resource usage and performance metrics (when available)

## LLM Analysis

The analysis includes:

- **Root Cause Analysis**: Identifies the underlying cause of the alert
- **Severity Assessment**: Evaluates the impact and urgency
- **Remediation Steps**: Provides actionable steps to resolve the issue
- **Prevention Recommendations**: Suggests ways to prevent similar issues

## Storage

Collected data is stored in PVCs for:

- **Historical Analysis**: Track patterns over time
- **Audit Trail**: Maintain records of incidents and resolutions
- **Trend Analysis**: Identify recurring issues and improvements
- **Compliance**: Meet regulatory and operational requirements

## Configuration

The tools require:

- **Kubernetes Access**: Proper RBAC permissions to read pod, event, and log data
- **LLM Model**: Configured model for analysis (OpenAI, Anthropic, etc.)
- **Storage**: PVC configuration for data persistence

## Security Considerations

- All data collection respects Kubernetes RBAC policies
- LLM analysis uses configured API keys and models
- Storage follows cluster security policies
- No sensitive data is logged or exposed in tool outputs 