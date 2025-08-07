# KAgent Chatbot Agent

## Overview

The KAgent Chatbot Agent is an intelligent conversational interface that provides Kubernetes alert analysis, remediation guidance, and operational insights by querying MongoDB-stored alert data.

## Features

### 🤖 **Intelligent Query Processing**
- **Natural Language Understanding**: Parses user queries to understand intent
- **Context-Aware Responses**: Provides relevant information based on query type
- **Smart Filtering**: Automatically applies filters based on query keywords

### 📊 **Alert Analysis & Reporting**
- **Recent Alerts**: "Show me issues in the last 3 hours"
- **Severity Filtering**: "What critical alerts do we have?"
- **Issue Type Analysis**: "Any pod crashes recently?"
- **Trend Analysis**: "What patterns do you see?"

### 🔧 **Remediation Support**
- **Script Generation**: Creates practical remediation scripts
- **Safety Checks**: Includes validation and rollback instructions
- **Monitoring Integration**: Provides verification steps

## Agent Description

```
KAgent Chatbot Agent

Purpose: An intelligent chatbot agent that provides Kubernetes alert analysis, 
remediation guidance, and operational insights by querying MongoDB-stored alert data.

Core Capabilities:
1. Alert Querying: Retrieve and summarize alerts from the last few hours/days
2. Issue Analysis: Provide detailed analysis of specific alert issues
3. Remediation Scripts: Generate practical remediation scripts for identified issues
4. Trend Analysis: Identify patterns and trends in alert data
5. Operational Guidance: Provide best practices and recommendations

Data Sources:
- MongoDB database (kagent-alerts collection)
- Real-time Kubernetes cluster data
- Historical alert patterns

Tools Available:
- chatbot_query: Get intelligent responses to alert queries
- get_remediation: Create remediation scripts for specific alerts

Response Format: Structured, actionable responses with clear next steps
```

## Available Tools

### 1. `chatbot_query`
**Purpose**: Intelligent chatbot for Kubernetes alert queries

**Parameters**:
- `query` (required): User query about alerts and issues
- `timeRange` (optional): Time range (e.g., '3h', '1d', '7d')
- `limit` (optional): Maximum number of alerts to analyze

**Example Queries**:
```bash
# Recent alerts
chatbot_query(query="Show me issues in the last 3 hours")

# Critical alerts
chatbot_query(query="What critical alerts do we have?", timeRange="1d")

# Pod issues
chatbot_query(query="Any pod crashes recently?", limit=10)

# Trend analysis
chatbot_query(query="What patterns do you see?", timeRange="7d")
```

### 2. `get_remediation`
**Purpose**: Get remediation script for specific alert

**Parameters**:
- `alertId` (required): Alert ID to get remediation for
- `service` (required): Service name
- `namespace` (required): Namespace

**Example Usage**:
```bash
get_remediation(
  alertId="test-crashing-pod-default-1722943743",
  service="test-crashing-pod", 
  namespace="default"
)
```

## Query Intent Recognition

The chatbot automatically recognizes different types of queries:

| Query Keywords | Intent | Action |
|---------------|--------|--------|
| "last", "recent" | `recent_alerts` | Show recent alerts |
| "critical", "high" | `high_severity` | Filter by severity |
| "crash", "pod" | `pod_issues` | Focus on pod problems |
| "service", "unavailable" | `service_issues` | Service-related issues |
| "resource", "memory", "cpu" | `resource_issues` | Resource problems |
| "trend", "pattern" | `trend_analysis` | Pattern analysis |
| "remediation", "fix" | `remediation_request` | Generate fixes |

## Data Structure

### MongoDB Schema
```json
{
  "_id": "ObjectId(...)",
  "alertId": "test-crashing-pod-default-1722943743",
  "targetService": "test-crashing-pod",
  "targetNamespace": "default",
  "severity": "Medium",
  "status": "Analyzed",
  "timestamp": "2025-08-06T01:29:03Z",
  "analysisResult": {
    "dataAnalysis": {
      "issueType": "PodCrash",
      "affectedResources": ["test-crashing-pod"],
      "dataQuality": "Good",
      "logAnalysis": "Pod is in CrashLoopBackOff state",
      "eventAnalysis": "Multiple restart events detected"
    },
    "severity": "Medium",
    "summary": "Pod is crashing repeatedly",
    "analysisTimestamp": "2025-08-06T01:29:04Z",
    "status": "data_analysis_complete"
  }
}
```

## Usage Examples

### 1. **General Inquiry**
```
User: "What's happening with our services?"
Response: "Based on the last 3 hours, I found 2 alerts:
- test-crashing-pod (Medium severity): Pod is crashing repeatedly
- web-service (Low severity): High memory usage detected

Key insights:
- 1 pod crash issue needs attention
- Memory usage patterns suggest resource optimization needed

Recommended actions:
1. Investigate the crashing pod immediately
2. Review resource limits for web-service"
```

### 2. **Specific Issue Analysis**
```
User: "Tell me about pod crashes"
Response: "I found 1 pod crash in the last 3 hours:

Alert: test-crashing-pod-default-1722943743
- Service: test-crashing-pod
- Severity: Medium
- Issue Type: PodCrash
- Summary: Pod is in CrashLoopBackOff state

Analysis:
- Log Analysis: Application error causing crashes
- Event Analysis: Multiple restart events detected
- Affected Resources: test-crashing-pod

This appears to be an application-level issue requiring code review."
```

### 3. **Remediation Request**
```
User: "Generate a fix for alert test-crashing-pod-default-1722943743"
Response: "Here's a remediation script for the pod crash issue:

#!/bin/bash
# Remediation script for test-crashing-pod

echo "Starting remediation for test-crashing-pod..."

# 1. Check current pod status
kubectl get pods -n default -l app=test-crashing-pod

# 2. Get detailed pod information
kubectl describe pod -n default -l app=test-crashing-pod

# 3. Check logs for root cause
kubectl logs -n default -l app=test-crashing-pod --tail=50

# 4. Restart the deployment (if safe)
kubectl rollout restart deployment/test-crashing-pod -n default

# 5. Monitor the restart
kubectl rollout status deployment/test-crashing-pod -n default

# 6. Verify the fix
kubectl get pods -n default -l app=test-crashing-pod

echo "Remediation complete. Monitor the pod for stability."
```

## Integration

### MCP Server Registration
The chatbot tools are automatically registered with the MCP server:

```go
// In tools/cmd/main.go
toolProviderMap := map[string]func(*server.MCPServer){
    "chatbot": func(s *server.MCPServer) { 
        chatbot.RegisterChatbotTools(s, llmModel, mongoClient, kubeconfig) 
    },
    // ... other tools
}
```

### Environment Variables
- `OPENAI_API_KEY`: Required for LLM responses
- `MONGODB_URI`: MongoDB connection string
- `KUBECONFIG`: Kubernetes configuration

## Error Handling

The chatbot includes robust error handling:

1. **LLM Failures**: Graceful fallback with alert summary
2. **MongoDB Issues**: Clear error messages
3. **Rate Limiting**: Data size limiting to prevent API limits
4. **Empty Results**: Helpful guidance when no alerts found

## Best Practices

### For Users:
1. **Be Specific**: "Show me critical alerts" vs "What's wrong?"
2. **Use Time Ranges**: "Last 3 hours" or "Past week"
3. **Request Remediation**: Ask for specific fixes when needed

### For Developers:
1. **Monitor Rate Limits**: Implement data size limiting
2. **Cache Responses**: Consider caching for repeated queries
3. **Log Interactions**: Track usage patterns for improvements

## Future Enhancements

1. **Multi-Cluster Support**: Query across multiple Kubernetes clusters
2. **Advanced Analytics**: Machine learning for pattern prediction
3. **Integration APIs**: Connect with external monitoring systems
4. **Custom Prompts**: Allow users to define custom analysis prompts
5. **Real-time Streaming**: Live alert streaming and notifications 