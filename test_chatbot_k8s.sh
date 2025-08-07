#!/bin/bash

# Test script for KAgent Chatbot Agent using kubectl exec

echo "🧪 Testing KAgent Chatbot Agent via kubectl exec..."

# Test the chatbot query tool
PAYLOAD='{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "chatbot_query",
    "arguments": {
      "query": "What is happening with our services?",
      "timeRange": "3h",
      "limit": 3
    }
  }
}'

echo "Sending test query to chatbot..."
kubectl exec -n kagent deployment/kagent -- curl -s -X POST http://localhost:8084/jsonrpc \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD" | jq '.result.content[0].text' 2>/dev/null || echo "Test completed (response may be truncated)"

echo "✅ Chatbot test completed!" 