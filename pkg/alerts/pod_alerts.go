package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/kagent-dev/tools/internal/telemetry"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// PodAlertData represents a pod-specific alert with rich metadata
type PodAlertData struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	AlertID   string             `bson:"alertId"`
	PodName   string             `bson:"podName"`
	Namespace string             `bson:"namespace"`
	Service   string             `bson:"service"`
	IssueType string             `bson:"issueType"` // e.g., "BackOff", "CrashLoop", "OOMKilled", "Pending"
	Severity  string             `bson:"severity"`  // Critical, High, Medium, Low
	Status    string             `bson:"status"`    // Collected, Analyzed, Resolved
	Phase     string             `bson:"phase"`     // Pod phase: Running, Pending, Failed, etc.
	Reason    string             `bson:"reason"`    // Pod reason: CrashLoopBackOff, OOMKilled, etc.
	Message   string             `bson:"message"`   // Human readable message

	// Timestamps
	CreatedAt   time.Time  `bson:"createdAt"`
	UpdatedAt   time.Time  `bson:"updatedAt"`
	CollectedAt time.Time  `bson:"collectedAt"`
	AnalyzedAt  *time.Time `bson:"analyzedAt,omitempty"`
	ResolvedAt  *time.Time `bson:"resolvedAt,omitempty"`

	// Pod-specific data
	PodEvents     []PodEvent          `bson:"podEvents,omitempty"`
	ContainerLogs map[string][]string `bson:"containerLogs,omitempty"`
	PodConditions []PodCondition      `bson:"podConditions,omitempty"`

	// Analysis data
	AnalysisResult map[string]interface{} `bson:"analysisResult,omitempty"`
	Remediation    string                 `bson:"remediation,omitempty"`
	RootCause      string                 `bson:"rootCause,omitempty"`

	// Metadata
	EventCount   int      `bson:"eventCount"`
	LogLineCount int      `bson:"logLineCount"`
	Tags         []string `bson:"tags,omitempty"`

	// Environment info
	ClusterName   string        `bson:"clusterName,omitempty"`
	NodeName      string        `bson:"nodeName,omitempty"`
	ResourceUsage ResourceUsage `bson:"resourceUsage,omitempty"`
}

// PodEvent represents a Kubernetes event related to a pod
type PodEvent struct {
	Type      string    `bson:"type"`   // Normal, Warning
	Reason    string    `bson:"reason"` // Scheduled, BackOff, etc.
	Message   string    `bson:"message"`
	Count     int32     `bson:"count"`
	FirstTime time.Time `bson:"firstTime"`
	LastTime  time.Time `bson:"lastTime"`
}

// PodCondition represents a pod condition
type PodCondition struct {
	Type    string `bson:"type"`
	Status  string `bson:"status"`
	Reason  string `bson:"reason"`
	Message string `bson:"message"`
}

// ResourceUsage represents pod resource usage
type ResourceUsage struct {
	CPURequest    string `bson:"cpuRequest,omitempty"`
	CPULimit      string `bson:"cpuLimit,omitempty"`
	MemoryRequest string `bson:"memoryRequest,omitempty"`
	MemoryLimit   string `bson:"memoryLimit,omitempty"`
	CPUUsage      string `bson:"cpuUsage,omitempty"`
	MemoryUsage   string `bson:"memoryUsage,omitempty"`
}

// RegisterPodAlertTools registers pod-specific alert tools
func RegisterPodAlertTools(s *server.MCPServer, mongoClient *mongo.Client, kubeconfig string) {
	fmt.Printf("DEBUG: RegisterPodAlertTools called for pod-specific alerts\n")

	// Create a tool instance with MongoDB client
	alertTool := &AlertCollectorTool{
		kubeconfig:  kubeconfig,
		mongoClient: mongoClient,
	}

	// Register store pod alert data tool
	s.AddTool(mcp.NewTool("store_pod_alert",
		mcp.WithDescription("Store pod-specific alert data in MongoDB"),
		mcp.WithString("podName", mcp.Description("Name of the pod"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the pod"), mcp.Required()),
		mcp.WithString("service", mcp.Description("Service name"), mcp.Required()),
		mcp.WithString("issueType", mcp.Description("Type of issue (BackOff, CrashLoop, etc.)"), mcp.Required()),
		mcp.WithString("severity", mcp.Description("Severity level"), mcp.Required()),
		mcp.WithString("podData", mcp.Description("Pod data (JSON string)")),
		mcp.WithString("events", mcp.Description("Pod events (JSON string)")),
		mcp.WithString("logs", mcp.Description("Container logs (JSON string)")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("store_pod_alert", alertTool.handleStorePodAlert)))

	fmt.Printf("DEBUG: store_pod_alert tool registered successfully\n")

	// Register query pod alerts tool
	s.AddTool(mcp.NewTool("query_pod_alerts",
		mcp.WithDescription("Query pod-specific alerts from MongoDB"),
		mcp.WithString("timeRange", mcp.Description("Time range filter (e.g., '3h', '1d', '7d')")),
		mcp.WithString("severity", mcp.Description("Severity filter (Critical, High, Medium, Low)")),
		mcp.WithString("issueType", mcp.Description("Issue type filter")),
		mcp.WithString("namespace", mcp.Description("Namespace filter")),
		mcp.WithString("service", mcp.Description("Service filter")),
		mcp.WithString("limit", mcp.Description("Maximum number of results to return")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("query_pod_alerts", alertTool.handleQueryPodAlerts)))

	fmt.Printf("DEBUG: query_pod_alerts tool registered successfully\n")

	// Register update pod alert analysis tool
	s.AddTool(mcp.NewTool("update_pod_alert_analysis",
		mcp.WithDescription("Update pod alert with analysis results"),
		mcp.WithString("alertId", mcp.Description("Alert ID to update"), mcp.Required()),
		mcp.WithString("analysisResult", mcp.Description("Analysis result (JSON string)")),
		mcp.WithString("remediation", mcp.Description("Remediation steps")),
		mcp.WithString("rootCause", mcp.Description("Root cause analysis")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("update_pod_alert_analysis", alertTool.handleUpdatePodAlertAnalysis)))

	fmt.Printf("DEBUG: update_pod_alert_analysis tool registered successfully\n")
	fmt.Printf("DEBUG: All pod alert tools registered successfully\n")
}

// handleStorePodAlert stores pod-specific alert data
func (a *AlertCollectorTool) handleStorePodAlert(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	fmt.Printf("DEBUG: handleStorePodAlert called with request: %+v\n", request)

	podName := mcp.ParseString(request, "podName", "")
	namespace := mcp.ParseString(request, "namespace", "")
	service := mcp.ParseString(request, "service", "")
	issueType := mcp.ParseString(request, "issueType", "")
	severity := mcp.ParseString(request, "severity", "")
	podDataStr := mcp.ParseString(request, "podData", "{}")
	eventsStr := mcp.ParseString(request, "events", "[]")
	logsStr := mcp.ParseString(request, "logs", "{}")

	now := time.Now()
	alertID := fmt.Sprintf("%s-%s-%s-%d", podName, namespace, issueType, now.Unix())

	// Parse pod data
	var podData map[string]interface{}
	if err := json.Unmarshal([]byte(podDataStr), &podData); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to parse pod data: %v", err)), nil
	}

	// Parse events
	var events []PodEvent
	if err := json.Unmarshal([]byte(eventsStr), &events); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to parse events: %v", err)), nil
	}

	// Parse logs
	var logs map[string][]string
	if err := json.Unmarshal([]byte(logsStr), &logs); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to parse logs: %v", err)), nil
	}

	// Extract pod phase and reason from pod data
	phase := "Unknown"
	reason := ""
	message := ""
	if status, ok := podData["status"].(map[string]interface{}); ok {
		if p, ok := status["phase"].(string); ok {
			phase = p
		}
		if r, ok := status["reason"].(string); ok {
			reason = r
		}
		if m, ok := status["message"].(string); ok {
			message = m
		}
	}

	// Calculate log line count
	logLineCount := 0
	for _, logLines := range logs {
		logLineCount += len(logLines)
	}

	// Create pod alert data
	podAlert := PodAlertData{
		AlertID:       alertID,
		PodName:       podName,
		Namespace:     namespace,
		Service:       service,
		IssueType:     issueType,
		Severity:      severity,
		Status:        "Collected",
		Phase:         phase,
		Reason:        reason,
		Message:       message,
		CreatedAt:     now,
		UpdatedAt:     now,
		CollectedAt:   now,
		PodEvents:     events,
		ContainerLogs: logs,
		EventCount:    len(events),
		LogLineCount:  logLineCount,
		Tags:          []string{"pod-alert", "kubernetes", issueType, severity},
	}

	// Store in MongoDB
	collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
	result, err := collection.InsertOne(ctx, podAlert)
	if err != nil {
		fmt.Printf("DEBUG: Failed to store pod alert data in MongoDB: %v\n", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to store pod alert: %v", err)), nil
	}

	fmt.Printf("DEBUG: Successfully stored pod alert data in MongoDB with ID: %v\n", result.InsertedID)

	return mcp.NewToolResultText(fmt.Sprintf("Pod alert stored successfully with ID: %s", alertID)), nil
}

// handleQueryPodAlerts queries pod-specific alerts
func (a *AlertCollectorTool) handleQueryPodAlerts(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	fmt.Printf("DEBUG: handleQueryPodAlerts called with request: %+v\n", request)

	timeRange := mcp.ParseString(request, "timeRange", "24h")
	severity := mcp.ParseString(request, "severity", "")
	issueType := mcp.ParseString(request, "issueType", "")
	namespace := mcp.ParseString(request, "namespace", "")
	service := mcp.ParseString(request, "service", "")
	limitStr := mcp.ParseString(request, "limit", "50")

	// Parse limit
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	// Build filter
	filter := bson.M{}

	// Time range filter
	if timeRange != "" {
		duration, err := parseTimeRange(timeRange)
		if err == nil {
			cutoff := time.Now().Add(-duration)
			filter["createdAt"] = bson.M{"$gte": cutoff}
		}
	}

	// Add other filters
	if severity != "" {
		filter["severity"] = severity
	}
	if issueType != "" {
		// Handle special case for "Failing" - map to all failure-related issue types
		if issueType == "Failing" {
			filter["issueType"] = bson.M{"$in": []string{"NotReady", "Failed", "CrashLoop", "Error", "Pending"}}
		} else {
			filter["issueType"] = issueType
		}
	}
	if namespace != "" {
		filter["namespace"] = namespace
	}
	if service != "" {
		filter["service"] = service
	}

	// Query MongoDB
	collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(int64(limit))

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to query pod alerts: %v", err)), nil
	}
	defer cursor.Close(ctx)

	var alerts []PodAlertData
	if err := cursor.All(ctx, &alerts); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to decode pod alerts: %v", err)), nil
	}

	// Convert to JSON
	resultJSON, err := json.MarshalIndent(alerts, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal results: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resultJSON)), nil
}

// handleUpdatePodAlertAnalysis updates pod alert with analysis results
func (a *AlertCollectorTool) handleUpdatePodAlertAnalysis(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	fmt.Printf("DEBUG: handleUpdatePodAlertAnalysis called with request: %+v\n", request)

	alertID := mcp.ParseString(request, "alertId", "")
	analysisResultStr := mcp.ParseString(request, "analysisResult", "{}")
	remediation := mcp.ParseString(request, "remediation", "")
	rootCause := mcp.ParseString(request, "rootCause", "")

	if alertID == "" {
		return mcp.NewToolResultError("Alert ID is required"), nil
	}

	// Parse analysis result
	var analysisResult map[string]interface{}
	if err := json.Unmarshal([]byte(analysisResultStr), &analysisResult); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to parse analysis result: %v", err)), nil
	}

	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"analysisResult": analysisResult,
			"remediation":    remediation,
			"rootCause":      rootCause,
			"status":         "Analyzed",
			"analyzedAt":     now,
			"updatedAt":      now,
		},
	}

	// Update in MongoDB
	collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
	result, err := collection.UpdateOne(ctx, bson.M{"alertId": alertID}, update)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to update pod alert: %v", err)), nil
	}

	if result.MatchedCount == 0 {
		return mcp.NewToolResultError(fmt.Sprintf("No pod alert found with ID: %s", alertID)), nil
	}

	fmt.Printf("DEBUG: Successfully updated pod alert analysis, modified count: %d\n", result.ModifiedCount)

	return mcp.NewToolResultText(fmt.Sprintf("Pod alert analysis updated successfully for ID: %s", alertID)), nil
}

// parseTimeRange parses time range string into duration
func parseTimeRange(timeRange string) (time.Duration, error) {
	switch timeRange {
	case "1h":
		return time.Hour, nil
	case "3h":
		return 3 * time.Hour, nil
	case "6h":
		return 6 * time.Hour, nil
	case "12h":
		return 12 * time.Hour, nil
	case "24h", "1d":
		return 24 * time.Hour, nil
	case "3d":
		return 3 * 24 * time.Hour, nil
	case "7d", "1w":
		return 7 * 24 * time.Hour, nil
	case "30d", "1m":
		return 30 * 24 * time.Hour, nil
	default:
		return 24 * time.Hour, fmt.Errorf("unsupported time range: %s", timeRange)
	}
}
