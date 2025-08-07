#!/usr/bin/env python3
"""
Simple test script for KAgent Chatbot Agent
"""

import requests
import json

def test_chatbot():
    """Test the chatbot agent functionality"""
    
    # Test the chatbot query tool
    print("🧪 Testing KAgent Chatbot Agent...")
    
    # Test 1: Basic query
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {
            "name": "chatbot_query",
            "arguments": {
                "query": "What's happening with our services?",
                "timeRange": "3h",
                "limit": 3
            }
        }
    }
    
    try:
        response = requests.post("http://localhost:8084/jsonrpc", json=payload, timeout=30)
        if response.status_code == 200:
            result = response.json()
            if "result" in result:
                print("✅ Chatbot query test passed!")
                print("Response:", result["result"]["content"][0]["text"][:200] + "...")
            else:
                print("❌ Chatbot query failed:", result.get("error", "Unknown error"))
        else:
            print(f"❌ HTTP error: {response.status_code}")
    except Exception as e:
        print(f"❌ Connection error: {e}")
        print("Make sure the MCP server is running on localhost:8084")

if __name__ == "__main__":
    test_chatbot() 