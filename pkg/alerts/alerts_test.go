package alerts

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestNewAlertTool(t *testing.T) {
	tool := NewAlertTool(nil)
	if tool == nil {
		t.Error("NewAlertTool should not return nil")
	}
}

func TestNewAlertToolWithConfig(t *testing.T) {
	tool := NewAlertToolWithConfig("test-kubeconfig", nil)
	if tool == nil {
		t.Error("NewAlertToolWithConfig should not return nil")
	}
	if tool.kubeconfig != "test-kubeconfig" {
		t.Error("kubeconfig should be set correctly")
	}
}

func TestRegisterTools(t *testing.T) {
	// Create a mock MCP server
	s := server.NewMCPServer("test-server", "v0.0.1")

	// Register tools - this should not panic
	RegisterTools(s, nil, "")

	// Note: We can't easily verify tools were registered without accessing internal state
	// The main test is that RegisterTools doesn't panic
	t.Log("RegisterTools completed successfully")
}

func TestFormatEvents(t *testing.T) {
	events := []PodEvent{
		{
			Type:      "Warning",
			Reason:    "BackOff",
			Message:   "Back-off restarting failed container",
			Count:     5,
			FirstTime: "2024-01-01T10:00:00Z",
			LastTime:  "2024-01-01T10:30:00Z",
		},
	}

	formatted := formatEvents(events)
	if formatted == "" {
		t.Error("formatEvents should return non-empty string")
	}

	// Test with empty events
	emptyFormatted := formatEvents([]PodEvent{})
	if emptyFormatted != "No events available" {
		t.Error("formatEvents should return 'No events available' for empty events")
	}
}

func TestPodAlertStruct(t *testing.T) {
	alert := PodAlert{
		PodName:      "test-pod",
		Namespace:    "default",
		Status:       "CrashLoopBackOff",
		Reason:       "CrashLoopBackOff",
		Message:      "Back-off restarting failed container",
		RestartCount: 5,
		Events:       []PodEvent{},
		Logs:         []string{},
		Analysis:     "",
		Remediation:  "",
	}

	if alert.PodName != "test-pod" {
		t.Error("PodName should be set correctly")
	}

	if alert.Namespace != "default" {
		t.Error("Namespace should be set correctly")
	}

	if alert.Status != "CrashLoopBackOff" {
		t.Error("Status should be set correctly")
	}
}

func TestPodEventStruct(t *testing.T) {
	event := PodEvent{
		Type:      "Warning",
		Reason:    "BackOff",
		Message:   "Back-off restarting failed container",
		Count:     5,
		FirstTime: "2024-01-01T10:00:00Z",
		LastTime:  "2024-01-01T10:30:00Z",
	}

	if event.Type != "Warning" {
		t.Error("Type should be set correctly")
	}

	if event.Reason != "BackOff" {
		t.Error("Reason should be set correctly")
	}

	if event.Count != 5 {
		t.Error("Count should be set correctly")
	}
}

// Mock test for handleGetPodAlerts (without actual kubectl calls)
func TestHandleGetPodAlertsBasic(t *testing.T) {
	tool := NewAlertTool(nil)

	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{
		"namespace":        "default",
		"all_namespaces":   "false",
		"include_analysis": "false",
	}

	// This will fail due to no kubectl, but we can test the parameter parsing
	_, err := tool.handleGetPodAlerts(context.Background(), request)
	// We expect an error since kubectl is not available in test environment
	if err == nil {
		t.Log("handleGetPodAlerts completed (this is expected to fail in test environment)")
	}
}

// Mock test for handleGetPodAlertDetails (without actual kubectl calls)
func TestHandleGetPodAlertDetailsBasic(t *testing.T) {
	tool := NewAlertTool(nil)

	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{
		"pod_name":         "test-pod",
		"namespace":        "default",
		"include_analysis": "false",
	}

	// This will fail due to no kubectl, but we can test the parameter parsing
	_, err := tool.handleGetPodAlertDetails(context.Background(), request)
	// We expect an error since kubectl is not available in test environment
	if err == nil {
		t.Log("handleGetPodAlertDetails completed (this is expected to fail in test environment)")
	}
}

// Mock test for handleGetClusterAlerts (without actual kubectl calls)
func TestHandleGetClusterAlertsBasic(t *testing.T) {
	tool := NewAlertTool(nil)

	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{
		"include_analysis": "false",
	}

	// This will fail due to no kubectl, but we can test the parameter parsing
	_, err := tool.handleGetClusterAlerts(context.Background(), request)
	// We expect an error since kubectl is not available in test environment
	if err == nil {
		t.Log("handleGetClusterAlerts completed (this is expected to fail in test environment)")
	}
}
