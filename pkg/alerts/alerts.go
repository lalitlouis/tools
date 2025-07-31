package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tmc/langchaingo/llms"

	"github.com/kagent-dev/tools/internal/commands"
	"github.com/kagent-dev/tools/internal/telemetry"
)

// AlertTool struct to hold the LLM model and kubeconfig
type AlertTool struct {
	kubeconfig string
	llmModel   llms.Model
}

// PodAlert represents a pod alert with details
type PodAlert struct {
	PodName      string     `json:"pod_name"`
	Namespace    string     `json:"namespace"`
	Status       string     `json:"status"`
	Reason       string     `json:"reason"`
	Message      string     `json:"message"`
	RestartCount int32      `json:"restart_count"`
	Age          string     `json:"age"`
	Events       []PodEvent `json:"events"`
	Logs         []string   `json:"logs"`
	Analysis     string     `json:"analysis"`
	Remediation  string     `json:"remediation"`
}

// PodEvent represents a Kubernetes event
type PodEvent struct {
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Count     int32  `json:"count"`
	FirstTime string `json:"first_time"`
	LastTime  string `json:"last_time"`
}

func NewAlertTool(llmModel llms.Model) *AlertTool {
	return &AlertTool{llmModel: llmModel}
}

func NewAlertToolWithConfig(kubeconfig string, llmModel llms.Model) *AlertTool {
	return &AlertTool{kubeconfig: kubeconfig, llmModel: llmModel}
}

// runKubectlCommand runs a kubectl command and returns the result
func (a *AlertTool) runKubectlCommand(ctx context.Context, args ...string) (*mcp.CallToolResult, error) {
	output, err := commands.NewCommandBuilder("kubectl").
		WithArgs(args...).
		WithKubeconfig(a.kubeconfig).
		Execute(ctx)

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

// runKubectlCommandString runs a kubectl command and returns just the string output
func (a *AlertTool) runKubectlCommandString(ctx context.Context, args ...string) (string, error) {
	output, err := commands.NewCommandBuilder("kubectl").
		WithArgs(args...).
		WithKubeconfig(a.kubeconfig).
		Execute(ctx)

	if err != nil {
		return "", err
	}

	return output, nil
}

// handleGetPodAlerts gets all pod alerts and analyzes them
func (a *AlertTool) handleGetPodAlerts(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "")
	allNamespaces := mcp.ParseString(request, "all_namespaces", "") == "true"
	includeAnalysis := mcp.ParseString(request, "include_analysis", "") == "true"

	// Get all pods with their status
	args := []string{"get", "pods", "-o", "json"}
	if allNamespaces {
		args = append(args, "--all-namespaces")
	} else if namespace != "" {
		args = append(args, "-n", namespace)
	}

	result, err := a.runKubectlCommandString(ctx, args...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get pods: %v", err)), nil
	}

	// Parse the JSON response
	var podList struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Status struct {
				Phase      string `json:"phase"`
				Conditions []struct {
					Type    string `json:"type"`
					Status  string `json:"status"`
					Reason  string `json:"reason"`
					Message string `json:"message"`
				} `json:"conditions"`
				ContainerStatuses []struct {
					RestartCount int32 `json:"restartCount"`
					Ready        bool  `json:"ready"`
					State        struct {
						Waiting struct {
							Reason  string `json:"reason"`
							Message string `json:"message"`
						} `json:"waiting"`
						Terminated struct {
							Reason   string `json:"reason"`
							Message  string `json:"message"`
							ExitCode int32  `json:"exitCode"`
						} `json:"terminated"`
					} `json:"state"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal([]byte(result), &podList); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to parse pod list: %v", err)), nil
	}

	var alerts []PodAlert

	// Process each pod to identify alerts
	for _, pod := range podList.Items {
		alert := PodAlert{
			PodName:   pod.Metadata.Name,
			Namespace: pod.Metadata.Namespace,
			Status:    pod.Status.Phase,
		}

		// Check if pod is in a problematic state
		isAlert := false
		if pod.Status.Phase == "Pending" || pod.Status.Phase == "Failed" || pod.Status.Phase == "Unknown" {
			isAlert = true
		}

		// Check container statuses
		for _, container := range pod.Status.ContainerStatuses {
			if !container.Ready {
				isAlert = true
				if container.State.Waiting.Reason != "" {
					alert.Reason = container.State.Waiting.Reason
					alert.Message = container.State.Waiting.Message
				} else if container.State.Terminated.Reason != "" {
					alert.Reason = container.State.Terminated.Reason
					alert.Message = container.State.Terminated.Message
				}
				alert.RestartCount = container.RestartCount
			}
		}

		// Check pod conditions
		for _, condition := range pod.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "False" {
				isAlert = true
				if alert.Reason == "" {
					alert.Reason = condition.Reason
					alert.Message = condition.Message
				}
			}
		}

		if isAlert {
			// Get pod events
			eventsResult, err := a.runKubectlCommandString(ctx, "get", "events", "-n", pod.Metadata.Namespace,
				"--field-selector", fmt.Sprintf("involvedObject.name=%s", pod.Metadata.Name), "-o", "json")
			if err == nil {
				var eventsList struct {
					Items []struct {
						Type      string `json:"type"`
						Reason    string `json:"reason"`
						Message   string `json:"message"`
						Count     int32  `json:"count"`
						FirstTime string `json:"firstTimestamp"`
						LastTime  string `json:"lastTimestamp"`
					} `json:"items"`
				}
				if err := json.Unmarshal([]byte(eventsResult), &eventsList); err == nil {
					for _, event := range eventsList.Items {
						alert.Events = append(alert.Events, PodEvent{
							Type:      event.Type,
							Reason:    event.Reason,
							Message:   event.Message,
							Count:     event.Count,
							FirstTime: event.FirstTime,
							LastTime:  event.LastTime,
						})
					}
				}
			}

			// Get pod logs if available
			logsResult, err := a.runKubectlCommandString(ctx, "logs", pod.Metadata.Name, "-n", pod.Metadata.Namespace, "--tail=50")
			if err == nil {
				alert.Logs = strings.Split(strings.TrimSpace(logsResult), "\n")
			}

			alerts = append(alerts, alert)
		}
	}

	// Generate analysis using LLM if requested
	if includeAnalysis && a.llmModel != nil && len(alerts) > 0 {
		for i := range alerts {
			analysis, err := a.generateAnalysis(ctx, alerts[i])
			if err == nil {
				alerts[i].Analysis = analysis
			}
		}
	}

	// Convert to JSON for response
	alertsJSON, err := json.MarshalIndent(alerts, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal alerts: %v", err)), nil
	}

	return mcp.NewToolResultText(string(alertsJSON)), nil
}

// generateAnalysis uses the LLM to analyze a pod alert
func (a *AlertTool) generateAnalysis(ctx context.Context, alert PodAlert) (string, error) {
	prompt := fmt.Sprintf(`Analyze this Kubernetes pod alert and provide insights:

Pod: %s
Namespace: %s
Status: %s
Reason: %s
Message: %s
Restart Count: %d

Events:
%s

Logs:
%s

Please provide:
1. Root cause analysis
2. Potential solutions
3. Prevention recommendations

Provide a concise but comprehensive analysis.`,
		alert.PodName, alert.Namespace, alert.Status, alert.Reason, alert.Message, alert.RestartCount,
		formatEvents(alert.Events), strings.Join(alert.Logs, "\n"))

	contents := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: prompt},
			},
		},
	}

	resp, err := a.llmModel.GenerateContent(ctx, contents, llms.WithModel("gpt-4o-mini"))
	if err != nil {
		return "", err
	}

	choices := resp.Choices
	if len(choices) < 1 {
		return "", fmt.Errorf("empty response from model")
	}
	c1 := choices[0]
	return c1.Content, nil
}

// formatEvents formats pod events for the prompt
func formatEvents(events []PodEvent) string {
	if len(events) == 0 {
		return "No events available"
	}

	var formatted []string
	for _, event := range events {
		formatted = append(formatted, fmt.Sprintf("- %s: %s (Count: %d, Last: %s)",
			event.Reason, event.Message, event.Count, event.LastTime))
	}
	return strings.Join(formatted, "\n")
}

// handleGetPodAlertDetails gets detailed information about a specific pod alert
func (a *AlertTool) handleGetPodAlertDetails(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := mcp.ParseString(request, "pod_name", "")
	namespace := mcp.ParseString(request, "namespace", "default")
	includeAnalysis := mcp.ParseString(request, "include_analysis", "") == "true"

	if podName == "" {
		return mcp.NewToolResultError("pod_name parameter is required"), nil
	}

	// Get pod details
	describeResult, err := a.runKubectlCommandString(ctx, "describe", "pod", podName, "-n", namespace)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to describe pod: %v", err)), nil
	}

	// Get pod logs
	logsResult, err := a.runKubectlCommandString(ctx, "logs", podName, "-n", namespace, "--tail=100")
	if err != nil {
		logsResult = "Unable to retrieve logs"
	}

	// Get pod events
	eventsResult, err := a.runKubectlCommandString(ctx, "get", "events", "-n", namespace,
		"--field-selector", fmt.Sprintf("involvedObject.name=%s", podName), "-o", "wide")
	if err != nil {
		eventsResult = "Unable to retrieve events"
	}

	// Combine all information
	details := fmt.Sprintf("Pod Details:\n%s\n\nLogs:\n%s\n\nEvents:\n%s",
		describeResult, logsResult, eventsResult)

	// Generate analysis if requested and LLM is available
	if includeAnalysis && a.llmModel != nil {
		analysis, err := a.generateDetailedAnalysis(ctx, podName, namespace, details)
		if err == nil {
			details += fmt.Sprintf("\n\nAI Analysis:\n%s", analysis)
		}
	}

	return mcp.NewToolResultText(details), nil
}

// generateDetailedAnalysis uses the LLM to analyze detailed pod information
func (a *AlertTool) generateDetailedAnalysis(ctx context.Context, podName, namespace, details string) (string, error) {
	prompt := fmt.Sprintf(`Analyze this Kubernetes pod in detail:

Pod: %s
Namespace: %s

Details:
%s

Please provide:
1. Root cause analysis
2. Specific remediation steps
3. Prevention strategies
4. Monitoring recommendations

Provide a detailed technical analysis with actionable steps.`, podName, namespace, details)

	contents := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: prompt},
			},
		},
	}

	resp, err := a.llmModel.GenerateContent(ctx, contents, llms.WithModel("gpt-4o-mini"))
	if err != nil {
		return "", err
	}

	choices := resp.Choices
	if len(choices) < 1 {
		return "", fmt.Errorf("empty response from model")
	}
	c1 := choices[0]
	return c1.Content, nil
}

// handleGetClusterAlerts gets alerts across the entire cluster
func (a *AlertTool) handleGetClusterAlerts(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	includeAnalysis := mcp.ParseString(request, "include_analysis", "") == "true"

	// Get all pods across all namespaces
	result, err := a.runKubectlCommandString(ctx, "get", "pods", "--all-namespaces", "-o", "wide")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get cluster pods: %v", err)), nil
	}

	// Parse the output to identify problematic pods
	lines := strings.Split(result, "\n")
	var alerts []PodAlert

	for _, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "NAME") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}

		namespace := fields[0]
		podName := fields[1]
		ready := fields[2]
		status := fields[3]
		_ = fields[4] // restarts - not used but keep for field alignment

		// Check if pod is problematic
		if status == "Pending" || status == "Failed" || status == "CrashLoopBackOff" ||
			status == "Error" || status == "ImagePullBackOff" || status == "ErrImagePull" ||
			strings.Contains(ready, "0/") {

			alert := PodAlert{
				PodName:   podName,
				Namespace: namespace,
				Status:    status,
				Reason:    "Pod not ready or in error state",
			}

			// Get more details for this pod
			describeResult, err := a.runKubectlCommandString(ctx, "describe", "pod", podName, "-n", namespace)
			if err == nil {
				alert.Message = describeResult
			}

			alerts = append(alerts, alert)
		}
	}

	// Generate cluster-wide analysis if requested
	if includeAnalysis && a.llmModel != nil && len(alerts) > 0 {
		clusterAnalysis, err := a.generateClusterAnalysis(ctx, alerts)
		if err == nil {
			// Add cluster analysis to the response
			alertsJSON, err := json.MarshalIndent(map[string]interface{}{
				"alerts":           alerts,
				"cluster_analysis": clusterAnalysis,
			}, "", "  ")
			if err == nil {
				return mcp.NewToolResultText(string(alertsJSON)), nil
			}
		}
	}

	// Convert to JSON for response
	alertsJSON, err := json.MarshalIndent(alerts, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal alerts: %v", err)), nil
	}

	return mcp.NewToolResultText(string(alertsJSON)), nil
}

// generateClusterAnalysis uses the LLM to analyze cluster-wide alerts
func (a *AlertTool) generateClusterAnalysis(ctx context.Context, alerts []PodAlert) (string, error) {
	alertSummary := fmt.Sprintf("Cluster Alert Summary:\nTotal Alerts: %d\n", len(alerts))

	for _, alert := range alerts {
		alertSummary += fmt.Sprintf("- %s/%s: %s (%s)\n",
			alert.Namespace, alert.PodName, alert.Status, alert.Reason)
	}

	prompt := fmt.Sprintf(`Analyze these Kubernetes cluster alerts:

%s

Please provide:
1. Common patterns or root causes
2. Cluster-wide remediation strategies
3. Infrastructure improvements needed
4. Monitoring and alerting recommendations

Provide a strategic analysis for cluster health improvement.`, alertSummary)

	contents := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: prompt},
			},
		},
	}

	resp, err := a.llmModel.GenerateContent(ctx, contents, llms.WithModel("gpt-4o-mini"))
	if err != nil {
		return "", err
	}

	choices := resp.Choices
	if len(choices) < 1 {
		return "", fmt.Errorf("empty response from model")
	}
	c1 := choices[0]
	return c1.Content, nil
}

// RegisterTools registers all alert tools with the MCP server
func RegisterTools(s *server.MCPServer, llm llms.Model, kubeconfig string) {
	alertTool := NewAlertToolWithConfig(kubeconfig, llm)

	s.AddTool(mcp.NewTool("alerts_get_pod_alerts",
		mcp.WithDescription("Get all pod alerts in a namespace or cluster"),
		mcp.WithString("namespace", mcp.Description("Namespace to check (optional, defaults to all)")),
		mcp.WithString("all_namespaces", mcp.Description("Check all namespaces (true/false)")),
		mcp.WithString("include_analysis", mcp.Description("Include AI analysis of alerts (true/false)")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("alerts_get_pod_alerts", alertTool.handleGetPodAlerts)))

	s.AddTool(mcp.NewTool("alerts_get_pod_alert_details",
		mcp.WithDescription("Get detailed information about a specific pod alert"),
		mcp.WithString("pod_name", mcp.Description("Name of the pod"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the pod (default: default)")),
		mcp.WithString("include_analysis", mcp.Description("Include AI analysis (true/false)")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("alerts_get_pod_alert_details", alertTool.handleGetPodAlertDetails)))

	s.AddTool(mcp.NewTool("alerts_get_cluster_alerts",
		mcp.WithDescription("Get all alerts across the entire cluster"),
		mcp.WithString("include_analysis", mcp.Description("Include AI analysis of cluster alerts (true/false)")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("alerts_get_cluster_alerts", alertTool.handleGetClusterAlerts)))
}
