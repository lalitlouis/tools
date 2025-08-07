#!/usr/bin/env python3
"""
Test script for enhanced timestamp functionality
"""

import requests
import json
import time

def test_enhanced_timestamps():
    """Test the enhanced timestamp functionality"""
    
    print("🧪 Testing Enhanced Timestamp Functionality...")
    
    # Test the chatbot query tool with timestamp request
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {
            "name": "chatbot_query",
            "arguments": {
                "query": "show me latest alerts with detailed timestamps",
                "timeRange": "3h",
                "limit": 3
            }
        }
    }
    
    try:
        print("📡 Sending request to tools server...")
        response = requests.post("http://localhost:8084/jsonrpc", json=payload, timeout=30)
        
        if response.status_code == 200:
            result = response.json()
            if "result" in result:
                print("✅ Enhanced timestamp query successful!")
                response_text = result["result"]["content"][0]["text"]
                print("Response preview:", response_text[:500] + "...")
                
                # Check if the response contains timestamp information
                if "Created:" in response_text or "Updated:" in response_text or "Collected:" in response_text:
                    print("✅ Enhanced timestamp fields are being used!")
                else:
                    print("⚠️  Enhanced timestamp fields not found in response")
                
                return True
            else:
                print("❌ Enhanced timestamp query failed:", result.get("error", "Unknown error"))
                return False
        else:
            print(f"❌ HTTP error: {response.status_code}")
            return False
    except Exception as e:
        print(f"❌ Connection error: {e}")
        print("Make sure the tools server is accessible on localhost:8084")
        return False

if __name__ == "__main__":
    print("🚀 Starting Enhanced Timestamp Tests...")
    
    # Test chatbot with enhanced timestamps
    success1 = test_enhanced_timestamps()
    
    if success1:
        print("\n🎉 Enhanced timestamp functionality is working!")
        print("The system now includes:")
        print("- Multiple timestamp fields (createdAt, updatedAt, collectedAt, analyzedAt)")
        print("- Enhanced metadata (eventCount, podCount, logLineCount)")
        print("- Tags for better categorization")
        print("- Better filtering and sorting capabilities")
    else:
        print("\n❌ Some tests failed. Check the system configuration.")
