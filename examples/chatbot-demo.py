#!/usr/bin/env python3
"""
KAgent Chatbot Agent Demo

This script demonstrates how to interact with the KAgent Chatbot Agent
through the MCP (Model Context Protocol) server.
"""

import json
import requests
import time
from typing import Dict, Any

class KAgentChatbotDemo:
    def __init__(self, base_url: str = "http://localhost:8084"):
        self.base_url = base_url
        self.session = requests.Session()
    
    def call_tool(self, tool_name: str, arguments: Dict[str, Any]) -> Dict[str, Any]:
        """Call a tool on the MCP server"""
        payload = {
            "jsonrpc": "2.0",
            "id": 1,
            "method": "tools/call",
            "params": {
                "name": tool_name,
                "arguments": arguments
            }
        }
        
        try:
            response = self.session.post(f"{self.base_url}/jsonrpc", json=payload)
            response.raise_for_status()
            return response.json()
        except requests.exceptions.RequestException as e:
            print(f"Error calling tool {tool_name}: {e}")
            return {"error": str(e)}
    
    def demo_chatbot_query(self):
        """Demonstrate chatbot query functionality"""
        print("🤖 KAgent Chatbot Agent Demo")
        print("=" * 50)
        
        # Example 1: General inquiry about recent alerts
        print("\n1. Query: 'What's happening with our services?'")
        result = self.call_tool("chatbot_query", {
            "query": "What's happening with our services?",
            "timeRange": "3h",
            "limit": 5
        })
        
        if "result" in result:
            print("Response:", result["result"]["content"][0]["text"])
        else:
            print("Error:", result.get("error", "Unknown error"))
        
        # Example 2: Specific issue analysis
        print("\n2. Query: 'Tell me about pod crashes'")
        result = self.call_tool("chatbot_query", {
            "query": "Tell me about pod crashes",
            "timeRange": "1d",
            "limit": 10
        })
        
        if "result" in result:
            print("Response:", result["result"]["content"][0]["text"])
        else:
            print("Error:", result.get("error", "Unknown error"))
        
        # Example 3: Critical alerts
        print("\n3. Query: 'What critical alerts do we have?'")
        result = self.call_tool("chatbot_query", {
            "query": "What critical alerts do we have?",
            "timeRange": "6h",
            "limit": 3
        })
        
        if "result" in result:
            print("Response:", result["result"]["content"][0]["text"])
        else:
            print("Error:", result.get("error", "Unknown error"))
    
    def demo_remediation(self):
        """Demonstrate remediation script generation"""
        print("\n🔧 Remediation Script Demo")
        print("=" * 50)
        
        # Example: Generate remediation for a specific alert
        print("\nGenerating remediation script for alert...")
        result = self.call_tool("get_remediation", {
            "alertId": "test-crashing-pod-default-1722943743",
            "service": "test-crashing-pod",
            "namespace": "default"
        })
        
        if "result" in result:
            print("Remediation Script:")
            print(result["result"]["content"][0]["text"])
        else:
            print("Error:", result.get("error", "Unknown error"))
    
    def demo_intent_recognition(self):
        """Demonstrate query intent recognition"""
        print("\n🧠 Intent Recognition Demo")
        print("=" * 50)
        
        test_queries = [
            "Show me issues in the last 3 hours",
            "What critical alerts do we have?",
            "Any pod crashes recently?",
            "Tell me about service issues",
            "What resource problems are there?",
            "Show me trends in the past week",
            "Generate a fix for the crashing pod"
        ]
        
        for query in test_queries:
            print(f"\nQuery: '{query}'")
            result = self.call_tool("chatbot_query", {
                "query": query,
                "timeRange": "3h",
                "limit": 3
            })
            
            if "result" in result:
                response = result["result"]["content"][0]["text"]
                # Truncate long responses for demo
                if len(response) > 200:
                    response = response[:200] + "..."
                print(f"Response: {response}")
            else:
                print(f"Error: {result.get('error', 'Unknown error')}")
    
    def run_full_demo(self):
        """Run the complete demo"""
        print("🚀 Starting KAgent Chatbot Agent Demo")
        print("Make sure the MCP server is running on localhost:8084")
        print("Press Enter to continue...")
        input()
        
        try:
            # Test basic connectivity
            response = self.session.get(f"{self.base_url}/health")
            if response.status_code == 200:
                print("✅ Connected to MCP server")
            else:
                print("❌ Failed to connect to MCP server")
                return
        except requests.exceptions.RequestException:
            print("❌ Cannot connect to MCP server. Is it running?")
            return
        
        # Run demos
        self.demo_chatbot_query()
        self.demo_remediation()
        self.demo_intent_recognition()
        
        print("\n🎉 Demo completed!")
        print("\nKey Features Demonstrated:")
        print("- Natural language query processing")
        print("- Intent recognition and filtering")
        print("- MongoDB data retrieval")
        print("- LLM-powered intelligent responses")
        print("- Remediation script generation")

if __name__ == "__main__":
    demo = KAgentChatbotDemo()
    demo.run_full_demo() 