package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kagent-dev/tools/internal/telemetry"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AlertData represents the structure of alert data stored in MongoDB
type AlertData struct {
	ID              primitive.ObjectID     `bson:"_id,omitempty"`
	AlertID         string                 `bson:"alertId"`
	TargetService   string                 `bson:"targetService"`
	TargetNamespace string                 `bson:"targetNamespace"`
	AlertType       string                 `bson:"alertType"`
	Severity        string                 `bson:"severity"`
	Status          string                 `bson:"status"`
	Timestamp       time.Time              `bson:"timestamp"`
	CollectionData  map[string]interface{} `bson:"collectionData"`
	AnalysisResult  map[string]interface{} `bson:"analysisResult"`
	Metadata        map[string]interface{} `bson:"metadata"`

	// Enhanced timestamp fields for better filtering and sorting
	CreatedAt   time.Time  `bson:"createdAt"`
	UpdatedAt   time.Time  `bson:"updatedAt"`
	CollectedAt time.Time  `bson:"collectedAt"`
	AnalyzedAt  *time.Time `bson:"analyzedAt,omitempty"`

	// Additional metadata fields
	DataSize     int `bson:"dataSize"`
	EventCount   int `bson:"eventCount"`
	PodCount     int `bson:"podCount"`
	LogLineCount int `bson:"logLineCount"`

	// Tags for better categorization
	Tags []string `bson:"tags,omitempty"`

	// Environment information
	ClusterName string `bson:"clusterName,omitempty"`
	NodeName    string `bson:"nodeName,omitempty"`
}

// RegisterMongoDBTools registers MongoDB-related tools with the MCP server
func RegisterMongoDBTools(s *server.MCPServer, mongoClient *mongo.Client, kubeconfig string) {
	fmt.Printf("DEBUG: RegisterMongoDBTools called for MongoDB tools\n")

	// Create a tool instance with MongoDB client
	alertTool := &AlertCollectorTool{
		kubeconfig:  kubeconfig,
		mongoClient: mongoClient,
	}

	// Register store alert data in MongoDB tool
	s.AddTool(mcp.NewTool("store_alert_data_mongodb",
		mcp.WithDescription("Store alert data in MongoDB database"),
		mcp.WithString("alertId", mcp.Description("Unique identifier for the alert"), mcp.Required()),
		mcp.WithString("targetService", mcp.Description("Target service name"), mcp.Required()),
		mcp.WithString("targetNamespace", mcp.Description("Target namespace"), mcp.Required()),
		mcp.WithString("alertType", mcp.Description("Type of alert (Pod, Service, etc.)"), mcp.Required()),
		mcp.WithString("collectionData", mcp.Description("Collected data from Kubernetes (JSON string)")),
		mcp.WithString("analysisResult", mcp.Description("AI analysis result (JSON string)")),
		mcp.WithString("metadata", mcp.Description("Additional metadata (JSON string)")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("store_alert_data_mongodb", alertTool.handleStoreAlertDataMongoDB)))

	fmt.Printf("DEBUG: store_alert_data_mongodb tool registered successfully\n")

	// Register query alert data from MongoDB tool
	s.AddTool(mcp.NewTool("query_alert_data_mongodb",
		mcp.WithDescription("Query alert data from MongoDB database"),
		mcp.WithString("query", mcp.Description("Query description (e.g., 'critical issues in last 3 hours')")),
		mcp.WithString("timeRange", mcp.Description("Time range filter (e.g., '3h', '1d', '7d')")),
		mcp.WithString("severity", mcp.Description("Severity filter (Critical, High, Medium, Low)")),
		mcp.WithString("service", mcp.Description("Service name filter")),
		mcp.WithString("limit", mcp.Description("Maximum number of results to return")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("query_alert_data_mongodb", alertTool.handleQueryAlertDataMongoDB)))

	fmt.Printf("DEBUG: query_alert_data_mongodb tool registered successfully\n")

	// Register get alert statistics from MongoDB tool
	s.AddTool(mcp.NewTool("get_alert_statistics_mongodb",
		mcp.WithDescription("Get alert statistics from MongoDB database"),
		mcp.WithString("timeRange", mcp.Description("Time range for statistics (e.g., '24h', '7d', '30d')")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("get_alert_statistics_mongodb", alertTool.handleGetAlertStatisticsMongoDB)))

	fmt.Printf("DEBUG: get_alert_statistics_mongodb tool registered successfully\n")
	fmt.Printf("DEBUG: All MongoDB tools registered successfully\n")
}

// handleStoreAlertDataMongoDB stores alert data in MongoDB
func (a *AlertCollectorTool) handleStoreAlertDataMongoDB(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	fmt.Printf("DEBUG: handleStoreAlertDataMongoDB called with request: %+v\n", request)

	alertID := mcp.ParseString(request, "alertId", "")
	targetService := mcp.ParseString(request, "targetService", "")
	targetNamespace := mcp.ParseString(request, "targetNamespace", "")
	alertType := mcp.ParseString(request, "alertType", "")
	collectionDataStr := mcp.ParseString(request, "collectionData", "{}")
	analysisResultStr := mcp.ParseString(request, "analysisResult", "{}")
	metadataStr := mcp.ParseString(request, "metadata", "{}")

	fmt.Printf("DEBUG: Parsed parameters - alertID: %s, targetService: %s, targetNamespace: %s\n", alertID, targetService, targetNamespace)

	if a.mongoClient == nil {
		fmt.Printf("DEBUG: MongoDB client is nil, returning error\n")
		return mcp.NewToolResultError("MongoDB client not available"), nil
	}

	// Parse JSON strings into maps
	var collectionData map[string]interface{}
	var analysisResult map[string]interface{}
	var metadata map[string]interface{}

	if err := json.Unmarshal([]byte(collectionDataStr), &collectionData); err != nil {
		fmt.Printf("DEBUG: Failed to parse collectionData: %v\n", err)
		collectionData = map[string]interface{}{}
	}

	if err := json.Unmarshal([]byte(analysisResultStr), &analysisResult); err != nil {
		fmt.Printf("DEBUG: Failed to parse analysisResult: %v\n", err)
		analysisResult = map[string]interface{}{}
	}

	if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
		fmt.Printf("DEBUG: Failed to parse metadata: %v\n", err)
		metadata = map[string]interface{}{}
	}

	// Create alert data document
	alertData := AlertData{
		AlertID:         alertID,
		TargetService:   targetService,
		TargetNamespace: targetNamespace,
		AlertType:       alertType,
		Timestamp:       time.Now(),
		CollectionData:  collectionData,
		AnalysisResult:  analysisResult,
		Metadata:        metadata,
	}

	// Determine severity from analysis result
	if severity, ok := analysisResult["severity"].(string); ok {
		alertData.Severity = severity
	} else {
		alertData.Severity = "Unknown"
	}

	// Determine status from analysis result
	if status, ok := analysisResult["status"].(string); ok {
		alertData.Status = status
	} else {
		alertData.Status = "Completed"
	}

	// Insert into MongoDB
	collection := a.mongoClient.Database("kagent-alerts").Collection("alerts")
	result, err := collection.InsertOne(ctx, alertData)
	if err != nil {
		fmt.Printf("DEBUG: Failed to insert alert data: %v\n", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to store alert data: %v", err)), nil
	}

	fmt.Printf("DEBUG: Successfully stored alert data with ID: %v\n", result.InsertedID)

	return mcp.NewToolResultText(fmt.Sprintf("Successfully stored alert data with ID: %v", result.InsertedID)), nil
}

// handleQueryAlertDataMongoDB queries alert data from MongoDB
func (a *AlertCollectorTool) handleQueryAlertDataMongoDB(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	fmt.Printf("DEBUG: handleQueryAlertDataMongoDB called with request: %+v\n", request)

	query := mcp.ParseString(request, "query", "")
	timeRange := mcp.ParseString(request, "timeRange", "")
	severity := mcp.ParseString(request, "severity", "")
	service := mcp.ParseString(request, "service", "")
	limit := mcp.ParseInt(request, "limit", 10)

	fmt.Printf("DEBUG: Parsed parameters - query: %s, timeRange: %s, severity: %s, service: %s, limit: %d\n", query, timeRange, severity, service, limit)

	if a.mongoClient == nil {
		fmt.Printf("DEBUG: MongoDB client is nil, returning error\n")
		return mcp.NewToolResultError("MongoDB client not available"), nil
	}

	// Build filter
	filter := bson.M{}

	// Add time range filter
	if timeRange != "" {
		var duration time.Duration
		var err error
		switch timeRange {
		case "1h":
			duration = time.Hour
		case "3h":
			duration = 3 * time.Hour
		case "6h":
			duration = 6 * time.Hour
		case "12h":
			duration = 12 * time.Hour
		case "1d":
			duration = 24 * time.Hour
		case "7d":
			duration = 7 * 24 * time.Hour
		case "30d":
			duration = 30 * 24 * time.Hour
		default:
			duration, err = time.ParseDuration(timeRange)
			if err != nil {
				fmt.Printf("DEBUG: Invalid time range: %s\n", timeRange)
				return mcp.NewToolResultError(fmt.Sprintf("Invalid time range: %s", timeRange)), nil
			}
		}
		filter["timestamp"] = bson.M{"$gte": time.Now().Add(-duration)}
	}

	// Add severity filter
	if severity != "" {
		filter["severity"] = severity
	}

	// Add service filter
	if service != "" {
		filter["targetService"] = service
	}

	// Set up options
	opts := options.Find().SetLimit(int64(limit)).SetSort(bson.D{{Key: "timestamp", Value: -1}})

	// Query MongoDB
	collection := a.mongoClient.Database("kagent-alerts").Collection("alerts")
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		fmt.Printf("DEBUG: Failed to query alert data: %v\n", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to query alert data: %v", err)), nil
	}
	defer cursor.Close(ctx)

	// Decode results
	var results []AlertData
	if err = cursor.All(ctx, &results); err != nil {
		fmt.Printf("DEBUG: Failed to decode results: %v\n", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to decode results: %v", err)), nil
	}

	fmt.Printf("DEBUG: Found %d alert records\n", len(results))

	// Format results
	var formattedResults []map[string]interface{}
	for _, result := range results {
		formattedResults = append(formattedResults, map[string]interface{}{
			"alertId":         result.AlertID,
			"targetService":   result.TargetService,
			"targetNamespace": result.TargetNamespace,
			"alertType":       result.AlertType,
			"severity":        result.Severity,
			"status":          result.Status,
			"timestamp":       result.Timestamp.Format(time.RFC3339),
			"analysisResult":  result.AnalysisResult,
		})
	}

	resultJSON, err := json.MarshalIndent(formattedResults, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal results: %v", err)), nil
	}
	return mcp.NewToolResultText(string(resultJSON)), nil
}

// handleGetAlertStatisticsMongoDB gets alert statistics from MongoDB
func (a *AlertCollectorTool) handleGetAlertStatisticsMongoDB(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	fmt.Printf("DEBUG: handleGetAlertStatisticsMongoDB called with request: %+v\n", request)

	timeRange := mcp.ParseString(request, "timeRange", "24h")

	fmt.Printf("DEBUG: Parsed parameters - timeRange: %s\n", timeRange)

	if a.mongoClient == nil {
		fmt.Printf("DEBUG: MongoDB client is nil, returning error\n")
		return mcp.NewToolResultError("MongoDB client not available"), nil
	}

	// Calculate time range
	var duration time.Duration
	var err error
	switch timeRange {
	case "1h":
		duration = time.Hour
	case "3h":
		duration = 3 * time.Hour
	case "6h":
		duration = 6 * time.Hour
	case "12h":
		duration = 12 * time.Hour
	case "1d":
		duration = 24 * time.Hour
	case "7d":
		duration = 7 * 24 * time.Hour
	case "30d":
		duration = 30 * 24 * time.Hour
	default:
		duration, err = time.ParseDuration(timeRange)
		if err != nil {
			fmt.Printf("DEBUG: Invalid time range: %s\n", timeRange)
			return mcp.NewToolResultError(fmt.Sprintf("Invalid time range: %s", timeRange)), nil
		}
	}

	// Build pipeline for aggregation
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"timestamp": bson.M{"$gte": time.Now().Add(-duration)}}}},
		{{Key: "$group", Value: bson.M{
			"_id":   "$severity",
			"count": bson.M{"$sum": 1},
		}}},
		{{Key: "$sort", Value: bson.M{"count": -1}}},
	}

	// Execute aggregation
	collection := a.mongoClient.Database("kagent-alerts").Collection("alerts")
	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		fmt.Printf("DEBUG: Failed to aggregate statistics: %v\n", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get statistics: %v", err)), nil
	}
	defer cursor.Close(ctx)

	// Decode results
	var results []bson.M
	if err = cursor.All(ctx, &results); err != nil {
		fmt.Printf("DEBUG: Failed to decode statistics: %v\n", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to decode statistics: %v", err)), nil
	}

	// Get total count
	totalFilter := bson.M{"timestamp": bson.M{"$gte": time.Now().Add(-duration)}}
	totalCount, err := collection.CountDocuments(ctx, totalFilter)
	if err != nil {
		fmt.Printf("DEBUG: Failed to get total count: %v\n", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get total count: %v", err)), nil
	}

	// Format statistics
	statistics := map[string]interface{}{
		"timeRange":         timeRange,
		"totalAlerts":       totalCount,
		"severityBreakdown": results,
	}

	fmt.Printf("DEBUG: Generated statistics for time range %s: %d total alerts\n", timeRange, totalCount)

	resultJSON, err := json.MarshalIndent(statistics, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal statistics: %v", err)), nil
	}
	return mcp.NewToolResultText(string(resultJSON)), nil
}
