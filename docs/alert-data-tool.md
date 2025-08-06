# Alert Data Tool

The Alert Data Tool provides chatbot access to collected alert data, enabling intelligent analysis, remediation script generation, and trend analysis.

## 🎯 **Purpose**

This tool allows the chatbot to:
- **Access collected alert data** from the Alert Collector Service
- **Analyze patterns and trends** in alert history
- **Generate remediation scripts** based on alert analysis
- **Provide intelligent recommendations** for issue resolution
- **Search through historical alerts** for similar issues

## 🛠️ **Features**

### **1. Alert History Access**
```bash
# Get alert history for a specific service
GET /alert-data?action=history&service=spire-server

# Response
{
  "service": "spire-server",
  "alerts": [
    {
      "serviceName": "spire-server",
      "alertTime": "2024-01-15T10:30:00Z",
      "alertType": "pod_crash",
      "podName": "spire-server-xyz123",
      "namespace": "spire-system",
      "logs": "Error: Out of memory...",
      "analysis": "Root cause: Memory pressure...",
      "rootCause": "Memory pressure caused pod termination",
      "recommendations": ["Increase memory limits", "Add resource monitoring"]
    }
  ],
  "count": 1
}
```

### **2. Recent Alerts**
```bash
# Get recent alerts across all services
GET /alert-data?action=recent&limit=5

# Response
{
  "alerts": [...],
  "count": 5,
  "limit": 5
}
```

### **3. Trend Analysis**
```bash
# Analyze trends for a service
GET /alert-data?action=trends&service=spire-server&days=30

# Response
{
  "service": "spire-server",
  "days": 30,
  "trends": {
    "totalAlerts": 15,
    "alertTypes": {
      "pod_crash": 8,
      "service_unavailable": 4,
      "high_resource_usage": 3
    },
    "commonRootCauses": {
      "Memory pressure": 6,
      "Network connectivity": 4,
      "Configuration error": 3
    },
    "timeDistribution": {
      "10": 3,
      "14": 5,
      "16": 7
    }
  }
}
```

### **4. Service Health**
```bash
# Get service health status
GET /alert-data?action=health&service=spire-server

# Response
{
  "serviceName": "spire-server",
  "alertsLast24h": 2,
  "lastAlertTime": "2024-01-15T14:20:00Z",
  "status": "unhealthy",
  "recommendations": [
    "Increase memory limits",
    "Add resource monitoring"
  ]
}
```

### **5. Search Alerts**
```bash
# Search through alert data
GET /alert-data?action=search&q=memory

# Response
{
  "query": "memory",
  "results": [
    {
      "serviceName": "spire-server",
      "alertType": "pod_crash",
      "logs": "Error: Out of memory...",
      "analysis": "Memory pressure caused pod termination"
    }
  ],
  "count": 1
}
```

### **6. Generate Remediation Scripts**
```bash
# Generate remediation script
POST /alert-data
{
  "action": "generate_remediation",
  "data": {
    "serviceName": "spire-server",
    "alertType": "pod_crash",
    "alertTime": "2024-01-15T10:30:00Z"
  }
}

# Response
{
  "script": {
    "name": "remediate-spire-server-pod_crash",
    "description": "Remediation script for spire-server pod crash",
    "type": "kubectl",
    "script": "#!/bin/bash\n# Remediation script for spire-server pod crash\n..."
  },
  "alert": {
    "serviceName": "spire-server",
    "alertType": "pod_crash",
    "logs": "Error: Out of memory...",
    "analysis": "Memory pressure caused pod termination"
  }
}
```

## 🤖 **Chatbot Integration**

### **Example Chatbot Conversations**

**User**: "Show me the alert history for spire-server"
**Chatbot**: Uses `get_alert_history` function to retrieve and display alert data

**User**: "What are the recent alerts across all services?"
**Chatbot**: Uses `get_recent_alerts` function to show recent alerts

**User**: "Analyze trends for spire-server over the last 30 days"
**Chatbot**: Uses `analyze_trends` function to provide trend analysis

**User**: "Generate a remediation script for the latest pod crash"
**Chatbot**: Uses `generate_remediation` function to create a kubectl script

**User**: "Search for alerts related to memory issues"
**Chatbot**: Uses `search_alerts` function to find memory-related alerts

**User**: "Show me the health status of spire-server"
**Chatbot**: Uses `get_service_health` function to show current health

## 🔧 **Implementation**

### **Tool Registration**
```go
// Register the tool with the chatbot
alertDataTool := alerts.NewAlertDataTool(k8sClient, "/alert-data")
handler := alerts.NewAlertDataHandler(alertDataTool)

// Add to chatbot tools
chatbot.AddTool("alert_data", handler)
```

### **Data Storage Structure**
```
/alert-data/
├── spire-server/
│   ├── 2024-01-15-10-30-crash/
│   │   ├── metadata.json
│   │   ├── logs.txt
│   │   ├── events.json
│   │   ├── metrics.json
│   │   └── analysis.md
│   └── 2024-01-15-14-20-service-down/
│       ├── metadata.json
│       ├── logs.txt
│       └── analysis.md
└── other-service/
    └── ...
```

## 📊 **Benefits**

### **1. Intelligent Analysis**
- **Pattern Recognition**: Identifies recurring issues
- **Root Cause Analysis**: Provides detailed analysis of problems
- **Trend Analysis**: Shows alert patterns over time

### **2. Automated Remediation**
- **Script Generation**: Creates kubectl scripts for common issues
- **Best Practices**: Incorporates Kubernetes best practices
- **Customizable**: Adapts scripts based on specific alert context

### **3. Proactive Monitoring**
- **Health Status**: Real-time service health monitoring
- **Early Warning**: Identifies potential issues before they become critical
- **Recommendations**: Provides actionable recommendations

### **4. Knowledge Base**
- **Historical Data**: Access to complete alert history
- **Search Capability**: Find similar issues and solutions
- **Learning**: Improves over time with more data

## 🚀 **Usage Examples**

### **Scenario 1: Pod Crash Analysis**
```
User: "The spire-server pod keeps crashing. What's happening?"

Chatbot: "Let me check the alert history for spire-server..."

[Uses get_alert_history to retrieve data]

Chatbot: "I found 3 pod crashes in the last 24 hours. The root cause is memory pressure. 
The pods are running out of memory and being terminated. Here's a remediation script:

kubectl scale deployment spire-server --replicas=2 -n spire-system
kubectl patch deployment spire-server -n spire-system -p '{"spec":{"template":{"spec":{"containers":[{"name":"spire-server","resources":{"limits":{"memory":"1Gi"}}}]}}}}'

Would you like me to analyze the trends to see if this is a recurring pattern?"
```

### **Scenario 2: Trend Analysis**
```
User: "Are there any patterns in our alert data?"

Chatbot: "Let me analyze the trends for all services..."

[Uses analyze_trends to get patterns]

Chatbot: "I found some interesting patterns:
- 70% of alerts occur between 2-4 PM
- Memory-related issues are the most common (40%)
- spire-server has the highest alert frequency

Recommendations:
1. Implement resource monitoring during peak hours
2. Add memory limits to all deployments
3. Set up proactive scaling for spire-server

Would you like me to generate a monitoring script for these patterns?"
```

### **Scenario 3: Remediation Script**
```
User: "Generate a script to fix the latest spire-server issue"

Chatbot: "I'll create a remediation script based on the latest alert..."

[Uses generate_remediation to create script]

Chatbot: "Here's a remediation script for the latest spire-server pod crash:

#!/bin/bash
# Remediation script for spire-server pod crash
# Generated on 2024-01-15T10:30:00Z

echo "Remediating spire-server pod crash..."

# Check pod status
kubectl get pods -n spire-system -l app=spire-server

# Check logs for errors
kubectl logs -n spire-system deployment/spire-server --tail=50

# Restart deployment if needed
kubectl rollout restart deployment/spire-server -n spire-system

# Wait for rollout
kubectl rollout status deployment/spire-server -n spire-system

echo "Remediation complete"

You can run this script to fix the current issue. Would you like me to also 
analyze why this keeps happening?"
```

## 🔗 **Integration with Alert Collector**

This tool works seamlessly with the Alert Collector Service:

1. **Alert Collector** collects and stores alert data
2. **Alert Data Tool** provides chatbot access to this data
3. **Chatbot** uses the tool to provide intelligent assistance
4. **Users** get actionable insights and automated remediation

The combination provides a complete alert management and resolution system! 