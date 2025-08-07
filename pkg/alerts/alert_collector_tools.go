package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tmc/langchaingo/llms"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kagent-dev/tools/internal/commands"
	"github.com/kagent-dev/tools/internal/telemetry"
)

// AlertCollectorTool provides tools for collecting and analyzing alert data
type AlertCollectorTool struct {
	kubeconfig  string
	llmModel    llms.Model
	mongoClient *mongo.Client
	jiraTool    *JiraIntegrationTool
}

// NewAlertCollectorTool creates a new alert collector tool
func NewAlertCollectorTool(kubeconfig string, llmModel llms.Model, mongoClient *mongo.Client) *AlertCollectorTool {
	return &AlertCollectorTool{
		kubeconfig:  kubeconfig,
		llmModel:    llmModel,
		mongoClient: mongoClient,
	}
}

// NewAlertCollectorToolWithJira creates a new alert collector tool with Jira integration
func NewAlertCollectorToolWithJira(kubeconfig string, llmModel llms.Model, mongoClient *mongo.Client, jiraConfig JiraSearchConfig) *AlertCollectorTool {
	return &AlertCollectorTool{
		kubeconfig:  kubeconfig,
		llmModel:    llmModel,
		mongoClient: mongoClient,
		jiraTool:    NewJiraIntegrationTool(jiraConfig),
	}
}

// runKubectlCommand runs a kubectl command and returns the result
func (a *AlertCollectorTool) runKubectlCommand(ctx context.Context, args ...string) (*mcp.CallToolResult, error) {
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
func (a *AlertCollectorTool) runKubectlCommandString(ctx context.Context, args ...string) (string, error) {
	output, err := commands.NewCommandBuilder("kubectl").
		WithArgs(args...).
		WithKubeconfig(a.kubeconfig).
		Execute(ctx)

	if err != nil {
		return "", err
	}

	return output, nil
}

// handleCollectAlertData collects comprehensive data for a target service
func (a *AlertCollectorTool) handleCollectAlertData(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	targetService := mcp.ParseString(request, "targetService", "")
	namespace := mcp.ParseString(request, "namespace", "")
	_ = mcp.ParseString(request, "collectPods", "true") == "true" // collectPods is always true for now
	collectEvents := mcp.ParseString(request, "collectEvents", "true") == "true"
	collectLogs := mcp.ParseString(request, "collectLogs", "true") == "true"
	maxLogLines := mcp.ParseString(request, "maxLogLines", "1000")

	// Collect pod data
	args := []string{"get", "pods", "-n", namespace, "-l", fmt.Sprintf("app=%s", targetService), "-o", "json"}
	result, err := a.runKubectlCommandString(ctx, args...)
	if err != nil {
		fmt.Printf("DEBUG: Failed to get pods for service %s in namespace %s: %v\n", targetService, namespace, err)
		// Try to get all pods in the namespace if the service-specific query fails
		args = []string{"get", "pods", "-n", namespace, "-o", "json"}
		result, err = a.runKubectlCommandString(ctx, args...)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get pods: %v", err)), nil
		}
		fmt.Printf("DEBUG: Retrieved all pods in namespace %s\n", namespace)
	} else {
		fmt.Printf("DEBUG: Retrieved pods for service %s in namespace %s\n", targetService, namespace)
	}

	// Parse pod data
	var podList struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Phase      string `json:"phase"`
				Reason     string `json:"reason"`
				Message    string `json:"message"`
				Conditions []struct {
					Type    string `json:"type"`
					Status  string `json:"status"`
					Reason  string `json:"reason"`
					Message string `json:"message"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal([]byte(result), &podList); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to parse pod data: %v", err)), nil
	}

	fmt.Printf("DEBUG: Found %d pods to process\n", len(podList.Items))
	for i, pod := range podList.Items {
		fmt.Printf("DEBUG: Pod %d: %s (Phase: %s)\n", i+1, pod.Metadata.Name, pod.Status.Phase)
	}

	// Build comprehensive data collection
	collectedData := map[string]interface{}{
		"targetService": targetService,
		"namespace":     namespace,
		"timestamp":     time.Now().Format(time.RFC3339),
		"pods":          podList.Items,
	}

	// Collect events if requested
	if collectEvents {
		eventsResult, err := a.runKubectlCommandString(ctx, "get", "events", "-n", namespace, "-o", "json")
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
				collectedData["events"] = eventsList.Items
				fmt.Printf("DEBUG: Collected %d events\n", len(eventsList.Items))
			} else {
				fmt.Printf("DEBUG: Failed to parse events: %v\n", err)
			}
		} else {
			fmt.Printf("DEBUG: Failed to collect events: %v\n", err)
		}
	}

	// Collect logs if requested
	if collectLogs {
		logs := make(map[string][]string)
		for _, pod := range podList.Items {
			logResult, err := a.runKubectlCommandString(ctx, "logs", pod.Metadata.Name, "-n", namespace, "--tail="+maxLogLines)
			if err == nil {
				logLines := strings.Split(strings.TrimSpace(logResult), "\n")
				logs[pod.Metadata.Name] = logLines
				fmt.Printf("DEBUG: Collected %d log lines for pod %s\n", len(logLines), pod.Metadata.Name)
			} else {
				fmt.Printf("DEBUG: Failed to collect logs for pod %s: %v\n", pod.Metadata.Name, err)
			}
		}
		collectedData["logs"] = logs
		fmt.Printf("DEBUG: Collected logs for %d pods\n", len(logs))
	}

	// Store in MongoDB if MongoDB client is available
	if a.mongoClient != nil {
		now := time.Now()
		alertID := fmt.Sprintf("%s-%s-%d", targetService, namespace, now.Unix())

		// Calculate metadata counts
		eventCount := 0
		if events, ok := collectedData["events"]; ok {
			// Handle different event data types
			switch eventList := events.(type) {
			case []interface{}:
				eventCount = len(eventList)
			case []struct {
				Type      string `json:"type"`
				Reason    string `json:"reason"`
				Message   string `json:"message"`
				Count     int32  `json:"count"`
				FirstTime string `json:"firstTimestamp"`
				LastTime  string `json:"lastTimestamp"`
			}:
				eventCount = len(eventList)
			default:
				// Try to get length using reflection
				v := reflect.ValueOf(events)
				if v.Kind() == reflect.Slice {
					eventCount = v.Len()
				} else {
					// Fallback: try to marshal and count
					if eventBytes, err := json.Marshal(events); err == nil {
						var eventArray []interface{}
						if json.Unmarshal(eventBytes, &eventArray) == nil {
							eventCount = len(eventArray)
						}
					}
				}
			}
		}

		podCount := len(podList.Items)
		logLineCount := 0
		if logs, ok := collectedData["logs"]; ok {
			if logMap, ok := logs.(map[string]interface{}); ok {
				for _, logLines := range logMap {
					if lines, ok := logLines.([]string); ok {
						logLineCount += len(lines)
					}
				}
			}
		}

		// Determine tags based on data
		tags := []string{"pod-alert", "kubernetes"}
		if podCount == 0 {
			tags = append(tags, "no-pods", "potential-crash")
		}
		if eventCount > 10 {
			tags = append(tags, "high-activity")
		}

		// Process each pod individually and create separate alerts
		fmt.Printf("DEBUG: Collected data keys: %v\n", getKeys(collectedData))

		// Get the actual pod list from the collected data
		podItems, ok := collectedData["pods"].([]struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Phase      string `json:"phase"`
				Reason     string `json:"reason"`
				Message    string `json:"message"`
				Conditions []struct {
					Type    string `json:"type"`
					Status  string `json:"status"`
					Reason  string `json:"reason"`
					Message string `json:"message"`
				} `json:"conditions"`
			} `json:"status"`
		})

		if !ok {
			fmt.Printf("DEBUG: No pods data found in collected data\n")
			// Fallback to old approach for backward compatibility
			alertData := AlertData{
				AlertID:         alertID,
				TargetService:   targetService,
				TargetNamespace: namespace,
				AlertType:       "Pod",
				Timestamp:       now,
				CollectionData:  collectedData,
				AnalysisResult:  map[string]interface{}{},
				Metadata: map[string]interface{}{
					"collectionTimestamp": now.Format(time.RFC3339),
					"dataSize":            len(collectedData),
				},
				Status:       "Collected",
				CreatedAt:    now,
				UpdatedAt:    now,
				CollectedAt:  now,
				DataSize:     len(collectedData),
				EventCount:   eventCount,
				PodCount:     podCount,
				LogLineCount: logLineCount,
				Tags:         tags,
			}
			collection := a.mongoClient.Database("kagent-alerts").Collection("alerts")
			result, err := collection.InsertOne(ctx, alertData)
			if err != nil {
				fmt.Printf("DEBUG: Failed to store alert data in MongoDB: %v\n", err)
			} else {
				fmt.Printf("DEBUG: Successfully stored alert data in MongoDB with ID: %v\n", result.InsertedID)
			}
		} else {
			// Process each pod individually
			for _, pod := range podItems {
				podName := pod.Metadata.Name

				// Determine issue type and severity based on pod status
				issueType := "Unknown"
				severity := "Low"

				// Use the pod status directly from the struct
				switch pod.Status.Phase {
				case "Pending":
					issueType = "Pending"
					severity = "Medium"
				case "Failed":
					issueType = "Failed"
					severity = "High"
				case "CrashLoopBackOff":
					issueType = "CrashLoop"
					severity = "High"
				case "Error":
					issueType = "Error"
					severity = "High"
				}

				// Check for specific conditions
				for _, condition := range pod.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == "False" {
						issueType = "NotReady"
						severity = "Medium"
						break
					}
				}

				// Get pod-specific events
				podEvents := []PodEvent{}
				if events, ok := collectedData["events"].([]interface{}); ok {
					for _, event := range events {
						if eventMap, ok := event.(map[string]interface{}); ok {
							if involvedObject, ok := eventMap["involvedObject"].(map[string]interface{}); ok {
								if name, ok := involvedObject["name"].(string); ok && name == podName {
									// Convert event to PodEvent
									podEvent := PodEvent{
										Type:    getNestedString(eventMap, "type"),
										Reason:  getNestedString(eventMap, "reason"),
										Message: getNestedString(eventMap, "message"),
										Count:   int32(getNestedInterface(eventMap, "count").(float64)),
									}
									if firstTime, ok := eventMap["firstTimestamp"].(string); ok {
										if t, err := time.Parse(time.RFC3339, firstTime); err == nil {
											podEvent.FirstTime = t
										}
									}
									if lastTime, ok := eventMap["lastTimestamp"].(string); ok {
										if t, err := time.Parse(time.RFC3339, lastTime); err == nil {
											podEvent.LastTime = t
										}
									}
									podEvents = append(podEvents, podEvent)
								}
							}
						}
					}
				}

				// Get pod-specific conditions
				podConditions := []PodCondition{}
				for _, condition := range pod.Status.Conditions {
					podCondition := PodCondition{
						Type:    condition.Type,
						Status:  condition.Status,
						Reason:  condition.Reason,
						Message: condition.Message,
					}
					podConditions = append(podConditions, podCondition)
				}

				// Get pod-specific logs
				containerLogs := make(map[string][]string)
				if logs, ok := collectedData["logs"].(map[string][]string); ok {
					if podLogs, ok := logs[podName]; ok {
						// Store logs under a default container name
						containerLogs["main"] = podLogs
					}
				}

				// Create pod-specific alert with complete collected data
				podAlert := PodAlertData{
					AlertID:       fmt.Sprintf("%s-%s-%s-%d", podName, namespace, issueType, now.Unix()),
					PodName:       podName,
					Namespace:     namespace,
					Service:       targetService,
					IssueType:     issueType,
					Severity:      severity,
					Status:        "Collected",
					Phase:         pod.Status.Phase,
					Reason:        pod.Status.Reason,
					Message:       pod.Status.Message,
					CreatedAt:     now,
					UpdatedAt:     now,
					CollectedAt:   now,
					PodEvents:     podEvents,
					ContainerLogs: containerLogs,
					PodConditions: podConditions,
					EventCount:    len(podEvents),
					LogLineCount:  len(containerLogs),
					Tags:          []string{"pod-alert", "kubernetes", issueType, severity},
					// Analysis data will be populated later by the analysis function
					AnalysisResult: map[string]interface{}{},
				}

				// Store or update pod alert in MongoDB (upsert to ensure only one entry per pod)
				collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")

				// Use upsert to ensure only one entry per pod
				filter := bson.M{
					"podName":   podName,
					"namespace": namespace,
					"service":   targetService,
				}

				update := bson.M{
					"$set": podAlert,
				}

				opts := options.Update().SetUpsert(true)
				result, err := collection.UpdateOne(ctx, filter, update, opts)
				if err != nil {
					fmt.Printf("DEBUG: Failed to store/update pod alert for %s: %v\n", podName, err)
				} else {
					if result.UpsertedCount > 0 {
						fmt.Printf("DEBUG: Successfully created new pod alert for %s with ID: %v\n", podName, result.UpsertedID)
					} else {
						fmt.Printf("DEBUG: Successfully updated existing pod alert for %s, modified count: %d\n", podName, result.ModifiedCount)
					}
				}
			}
		}
	}

	// Convert to JSON
	resultJSON, err := json.MarshalIndent(collectedData, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resultJSON)), nil
}

// handleAnalyzeAlertData performs LLM analysis on collected alert data
func (a *AlertCollectorTool) handleAnalyzeAlertData(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	fmt.Printf("DEBUG: handleAnalyzeAlertData called with request: %+v\n", request)

	targetService := mcp.ParseString(request, "targetService", "")
	targetNamespace := mcp.ParseString(request, "targetNamespace", "")

	fmt.Printf("DEBUG: Parsed parameters - targetService: %s, targetNamespace: %s\n", targetService, targetNamespace)

	if a.llmModel == nil {
		fmt.Printf("DEBUG: LLM model is nil, providing fallback analysis\n")
		// Provide a fallback analysis when LLM is not available
		fallbackAnalysis := `{
			"summary": "Alert data collected successfully but LLM analysis is not available due to missing OpenAI API key",
			"severity": "Medium",
			"rootCause": "LLM model not configured - please set OPENAI_API_KEY environment variable",
			"remediation": [
				"1. Set OPENAI_API_KEY environment variable in the tool server deployment",
				"2. Create a Kubernetes secret with your OpenAI API key",
				"3. Restart the tool server deployment"
			],
			"prevention": [
				"Ensure OpenAI API key is properly configured before deploying alert collectors",
				"Consider using a secrets management solution for API keys"
			],
			"status": "fallback_analysis"
		}`
		return mcp.NewToolResultText(fallbackAnalysis), nil
	}

	fmt.Printf("DEBUG: LLM model is available, proceeding with analysis\n")

	// Get the collected data from MongoDB instead of collecting it again
	fmt.Printf("DEBUG: Retrieving collected data from MongoDB for analysis\n")

	if a.mongoClient == nil {
		return mcp.NewToolResultError("MongoDB client not available"), nil
	}

	// Find the most recent pod alert documents for this service
	collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
	filter := bson.M{
		"service":   targetService,
		"namespace": targetNamespace,
	}

	var podAlertData PodAlertData
	err := collection.FindOne(ctx, filter).Decode(&podAlertData)
	if err != nil {
		fmt.Printf("DEBUG: Failed to find collected data in MongoDB: %v\n", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to find collected data: %v", err)), nil
	}

	fmt.Printf("DEBUG: Found collected data in MongoDB, proceeding with analysis\n")

	// Use the collected data from MongoDB
	collectedDataBytes, err := json.Marshal(podAlertData)
	if err != nil {
		fmt.Printf("DEBUG: Failed to marshal collected data: %v\n", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal collected data: %v", err)), nil
	}

	actualData := string(collectedDataBytes)
	fmt.Printf("DEBUG: Using collected data from MongoDB for analysis\n")

	// Create analysis prompt focused on data analysis, not remediation
	// Limit data size to avoid rate limiting
	limitedData := limitDataSize(actualData, 2000) // Limit to 2000 characters

	prompt := fmt.Sprintf(`Analyze this Kubernetes data and provide a JSON response:

{
  "dataAnalysis": {
    "issueType": "PodCrash/ServiceUnavailable/ResourceExhaustion/etc",
    "affectedResources": ["pod-name"],
    "dataQuality": "Good/Fair/Poor",
    "logAnalysis": "Brief summary of log patterns",
    "eventAnalysis": "Brief summary of key events"
  },
  "severity": "Low/Medium/High/Critical",
  "summary": "Brief technical summary",
  "analysisTimestamp": "%s",
  "status": "data_analysis_complete"
}

Data: %s

Analyze the data structure and patterns. Respond with ONLY valid JSON.`, time.Now().Format(time.RFC3339), limitedData)

	fmt.Printf("DEBUG: Created prompt for LLM analysis with actual data\n")

	// Add a timeout context for the LLM call
	llmCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	fmt.Printf("DEBUG: Calling LLM with timeout\n")

	// Perform LLM analysis
	resp, err := llms.GenerateFromSinglePrompt(llmCtx, a.llmModel, prompt, llms.WithModel("gpt-4o-mini"))
	if err != nil {
		fmt.Printf("DEBUG: LLM analysis failed: %v\n", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to analyze data with LLM: %v", err)), nil
	}

	fmt.Printf("DEBUG: LLM analysis completed successfully\n")

	// Clean the LLM response to remove markdown code blocks if present
	cleanedResp := cleanLLMResponse(resp)
	fmt.Printf("DEBUG: Cleaned LLM response: %s\n", cleanedResp)

	// Update MongoDB document with analysis result if MongoDB client is available
	if a.mongoClient != nil {
		// Parse the analysis result to extract severity
		var analysisData map[string]interface{}
		if err := json.Unmarshal([]byte(cleanedResp), &analysisData); err != nil {
			fmt.Printf("DEBUG: Failed to parse analysis result: %v\n", err)
			fmt.Printf("DEBUG: Raw LLM response: %s\n", resp)

			// Create a fallback analysis structure
			analysisData = map[string]interface{}{
				"dataAnalysis": map[string]interface{}{
					"issueType":         "Unknown",
					"affectedResources": []string{},
					"dataQuality":       "Poor",
					"logAnalysis":       "Unable to parse LLM response",
					"eventAnalysis":     "Analysis failed",
					"metricsAnalysis":   "No metrics available",
				},
				"severity":          "Medium",
				"summary":           "Analysis completed but response format was invalid",
				"analysisTimestamp": time.Now().Format(time.RFC3339),
				"status":            "data_analysis_with_errors",
				"rawResponse":       resp,
			}
		}

		// Update existing pod alerts with analysis results
		fmt.Printf("DEBUG: Starting pod alert analysis update process\n")

		// Find existing pod alerts for this service and namespace
		podCollection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
		podFilter := bson.M{
			"service":   targetService,
			"namespace": targetNamespace,
		}

		now := time.Now()

		cursor, cursorErr := podCollection.Find(ctx, podFilter)
		if cursorErr != nil {
			fmt.Printf("DEBUG: Failed to find existing pod alerts: %v\n", cursorErr)
		} else {
			defer cursor.Close(ctx)
			var existingPodAlerts []PodAlertData
			if decodeErr := cursor.All(ctx, &existingPodAlerts); decodeErr != nil {
				fmt.Printf("DEBUG: Failed to decode existing pod alerts: %v\n", decodeErr)
			} else {
				fmt.Printf("DEBUG: Found %d existing pod alerts to update with analysis\n", len(existingPodAlerts))

				// Update each existing pod alert with analysis results
				for _, podAlert := range existingPodAlerts {
					// Create pod-specific analysis data
					podAnalysisData := map[string]interface{}{
						"dataAnalysis": map[string]interface{}{
							"issueType":         analysisData["dataAnalysis"].(map[string]interface{})["issueType"],
							"affectedResources": []string{podAlert.PodName},
							"dataQuality":       analysisData["dataAnalysis"].(map[string]interface{})["dataQuality"],
							"logAnalysis":       fmt.Sprintf("Pod %s: %s", podAlert.PodName, getNestedString(analysisData, "dataAnalysis.logAnalysis")),
							"eventAnalysis":     fmt.Sprintf("Pod %s: %s", podAlert.PodName, getNestedString(analysisData, "dataAnalysis.eventAnalysis")),
						},
						"severity":          analysisData["severity"],
						"summary":           fmt.Sprintf("Pod %s: %s", podAlert.PodName, getNestedString(analysisData, "summary")),
						"analysisTimestamp": now.Format(time.RFC3339),
						"status":            "data_analysis_complete",
					}

					// Extract root cause and remediation if available
					rootCause := getNestedString(analysisData, "rootCause")
					remediation := getNestedString(analysisData, "remediation")

					// Update the pod alert with analysis results
					update := bson.M{
						"$set": bson.M{
							"analysisResult": podAnalysisData,
							"status":         "Analyzed",
							"severity":       analysisData["severity"],
							"rootCause":      rootCause,
							"remediation":    remediation,
							"analyzedAt":     now,
							"updatedAt":      now,
						},
					}

					updateResult, updateErr := podCollection.UpdateOne(ctx, bson.M{"_id": podAlert.ID}, update)
					if updateErr != nil {
						fmt.Printf("DEBUG: Failed to update pod alert %s with analysis: %v\n", podAlert.PodName, updateErr)
					} else {
						fmt.Printf("DEBUG: Successfully updated pod alert %s with analysis, modified count: %d\n", podAlert.PodName, updateResult.ModifiedCount)
					}
				}
			}
		}
	}

	return mcp.NewToolResultText(resp), nil
}

// handleQueryAlerts queries alerts for chatbot interactions
func (a *AlertCollectorTool) handleQueryAlerts(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	timeRange := mcp.ParseString(request, "timeRange", "24h")
	severity := mcp.ParseString(request, "severity", "")
	service := mcp.ParseString(request, "service", "")
	limit := mcp.ParseInt(request, "limit", 10)

	if a.mongoClient == nil {
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
			return mcp.NewToolResultError(fmt.Sprintf("Invalid time range: %s", timeRange)), nil
		}
	}

	// Build filter with enhanced timestamp fields
	filter := bson.M{
		"$or": []bson.M{
			{"createdAt": bson.M{"$gte": time.Now().Add(-duration)}},
			{"collectedAt": bson.M{"$gte": time.Now().Add(-duration)}},
			{"updatedAt": bson.M{"$gte": time.Now().Add(-duration)}},
		},
	}

	if severity != "" {
		filter["severity"] = severity
	}

	if service != "" {
		filter["targetService"] = service
	}

	// Set up options with enhanced timestamp sorting
	opts := options.Find().SetLimit(int64(limit)).SetSort(bson.D{{Key: "updatedAt", Value: -1}})

	// Query MongoDB
	collection := a.mongoClient.Database("kagent-alerts").Collection("alerts")
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to query alerts: %v", err)), nil
	}
	defer cursor.Close(ctx)

	// Decode results
	var alerts []map[string]interface{}
	if err := cursor.All(ctx, &alerts); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to decode alerts: %v", err)), nil
	}

	// Format results for chatbot
	var results []map[string]interface{}
	for _, alert := range alerts {
		// Extract key information for chatbot
		result := map[string]interface{}{
			"alertId":         alert["alertId"],
			"targetService":   alert["targetService"],
			"targetNamespace": alert["targetNamespace"],
			"severity":        alert["severity"],
			"status":          alert["status"],
			"timestamp":       alert["timestamp"],
			"summary":         getNestedString(alert["analysisResult"].(map[string]interface{}), "summary"),
			"issueType":       getNestedString(alert["analysisResult"].(map[string]interface{}), "dataAnalysis.issueType"),
		}
		results = append(results, result)
	}

	resultJSON, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal results: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resultJSON)), nil
}

// Helper functions for accessing nested map values
func getNestedString(data map[string]interface{}, path string) string {
	keys := strings.Split(path, ".")
	current := data

	for _, key := range keys {
		if val, ok := current[key]; ok {
			if nested, ok := val.(map[string]interface{}); ok {
				current = nested
			} else if str, ok := val.(string); ok {
				return str
			}
		}
	}
	return "Unknown"
}

func getNestedInterface(data map[string]interface{}, path string) interface{} {
	keys := strings.Split(path, ".")
	current := data

	for _, key := range keys {
		if val, ok := current[key]; ok {
			if nested, ok := val.(map[string]interface{}); ok {
				current = nested
			} else {
				return val
			}
		}
	}
	return nil
}

// limitDataSize limits the data size to avoid rate limiting
func limitDataSize(data string, maxLength int) string {
	if len(data) <= maxLength {
		return data
	}

	// Try to find a good truncation point
	truncated := data[:maxLength]

	// Try to end at a complete word or line
	if lastNewline := strings.LastIndex(truncated, "\n"); lastNewline > maxLength*3/4 {
		truncated = truncated[:lastNewline]
	} else if lastSpace := strings.LastIndex(truncated, " "); lastSpace > maxLength*3/4 {
		truncated = truncated[:lastSpace]
	}

	return truncated + "... [truncated]"
}

// cleanLLMResponse removes markdown code blocks from LLM responses
func cleanLLMResponse(response string) string {
	// Remove markdown code blocks if present
	if strings.HasPrefix(strings.TrimSpace(response), "```json") {
		// Find the end of the code block
		lines := strings.Split(response, "\n")
		var cleanedLines []string
		inCodeBlock := false

		for _, line := range lines {
			if strings.TrimSpace(line) == "```json" {
				inCodeBlock = true
				continue
			}
			if strings.TrimSpace(line) == "```" && inCodeBlock {
				inCodeBlock = false
				continue
			}
			if inCodeBlock {
				cleanedLines = append(cleanedLines, line)
			}
		}

		return strings.Join(cleanedLines, "\n")
	}

	// If no code blocks, return as is
	return response
}

// getKeys returns the keys of a map as a slice of strings
func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// handleGenerateRemediationScript generates a remediation script based on alert analysis
func (a *AlertCollectorTool) handleGenerateRemediationScript(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	alertID := mcp.ParseString(request, "alertId", "")
	targetService := mcp.ParseString(request, "targetService", "")
	targetNamespace := mcp.ParseString(request, "targetNamespace", "")

	if a.llmModel == nil {
		return mcp.NewToolResultError("LLM model not available"), nil
	}

	// Fetch the alert data from MongoDB
	if a.mongoClient == nil {
		return mcp.NewToolResultError("MongoDB client not available"), nil
	}

	collection := a.mongoClient.Database("kagent-alerts").Collection("alerts")
	filter := bson.M{
		"alertId":         alertID,
		"targetService":   targetService,
		"targetNamespace": targetNamespace,
	}

	var alertData AlertData
	err := collection.FindOne(ctx, filter).Decode(&alertData)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to find alert data: %v", err)), nil
	}

	// Create remediation prompt based on the stored analysis
	prompt := fmt.Sprintf(`Based on this alert analysis, generate a practical remediation script:

Alert ID: %s
Service: %s
Namespace: %s
Severity: %s
Issue Type: %s
Summary: %s

Data Analysis:
- Log Analysis: %s
- Event Analysis: %s
- Affected Resources: %v

Please generate a remediation script that:
1. Addresses the specific issue type identified
2. Includes safety checks and validation
3. Provides rollback instructions
4. Includes monitoring and verification steps
5. Uses kubectl commands where appropriate

Format the script with clear comments and error handling.`,
		alertID,
		targetService,
		targetNamespace,
		alertData.Severity,
		getNestedString(alertData.AnalysisResult, "dataAnalysis.issueType"),
		getNestedString(alertData.AnalysisResult, "summary"),
		getNestedString(alertData.AnalysisResult, "dataAnalysis.logAnalysis"),
		getNestedString(alertData.AnalysisResult, "dataAnalysis.eventAnalysis"),
		getNestedInterface(alertData.AnalysisResult, "dataAnalysis.affectedResources"))

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
		return mcp.NewToolResultError(fmt.Sprintf("Failed to generate remediation script: %v", err)), nil
	}

	if len(resp.Choices) == 0 {
		return mcp.NewToolResultError("Empty response from LLM"), nil
	}

	return mcp.NewToolResultText(resp.Choices[0].Content), nil
}

// handleStoreAlertData stores collected data to a file (simulating PVC storage)
func (a *AlertCollectorTool) handleStoreAlertData(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	data := mcp.ParseString(request, "data", "")
	storagePath := mcp.ParseString(request, "storagePath", "/tmp/alert-data")

	// For now, we'll just return success since we can't write to PVC from tools
	// In a real implementation, this would write to the PVC mount point
	result := map[string]interface{}{
		"status":      "stored",
		"storagePath": storagePath,
		"timestamp":   time.Now().Format(time.RFC3339),
		"dataSize":    len(data),
	}

	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resultJSON)), nil
}

// formatDataForPrompt formats data for inclusion in analysis prompts
func formatDataForPrompt(data interface{}) string {
	if data == nil {
		return "No data available"
	}

	dataJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error formatting data: %v", err)
	}

	return string(dataJSON)
}

// RegisterTools registers all Alert Collector tools with the MCP server
func RegisterTools(s *server.MCPServer, llm llms.Model, kubeconfig string, mongoClient *mongo.Client) {
	fmt.Printf("DEBUG: RegisterTools called for alerts package\n")
	alertTool := NewAlertCollectorTool(kubeconfig, llm, mongoClient)

	fmt.Printf("DEBUG: About to register collect_alert_data tool\n")

	// Register collect alert data tool
	s.AddTool(mcp.NewTool("collect_alert_data",
		mcp.WithDescription("Collect comprehensive data for a target service including pods, events, logs, and metrics"),
		mcp.WithString("targetService", mcp.Description("Name of the target service to collect data for"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace where the target service is running"), mcp.Required()),
		mcp.WithString("collectPods", mcp.Description("Whether to collect pod information (true/false)")),
		mcp.WithString("collectEvents", mcp.Description("Whether to collect events (true/false)")),
		mcp.WithString("collectLogs", mcp.Description("Whether to collect logs (true/false)")),
		mcp.WithString("maxLogLines", mcp.Description("Maximum number of log lines to collect")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("collect_alert_data", alertTool.handleCollectAlertData)))

	fmt.Printf("DEBUG: collect_alert_data tool registered successfully\n")

	// Register analyze alert data tool
	s.AddTool(mcp.NewTool("analyze_alert_data",
		mcp.WithDescription("Perform LLM analysis on collected alert data"),
		mcp.WithString("targetService", mcp.Description("Name of the target service to analyze"), mcp.Required()),
		mcp.WithString("targetNamespace", mcp.Description("Namespace of the target service"), mcp.Required()),
		mcp.WithString("promptTemplate", mcp.Description("Custom prompt template for analysis")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("analyze_alert_data", alertTool.handleAnalyzeAlertData)))

	fmt.Printf("DEBUG: analyze_alert_data tool registered successfully\n")

	// Register generate remediation script tool
	s.AddTool(mcp.NewTool("generate_remediation_script",
		mcp.WithDescription("Generate a remediation script based on alert analysis"),
		mcp.WithString("analysis", mcp.Description("Alert analysis result (JSON string)"), mcp.Required()),
		mcp.WithString("alertType", mcp.Description("Type of alert (e.g., PodCrash, ServiceUnavailable)"), mcp.Required()),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("generate_remediation_script", alertTool.handleGenerateRemediationScript)))

	fmt.Printf("DEBUG: generate_remediation_script tool registered successfully\n")

	// Register store alert data tool
	s.AddTool(mcp.NewTool("store_alert_data",
		mcp.WithDescription("Store collected alert data to storage"),
		mcp.WithString("data", mcp.Description("Collected alert data to store (JSON string)"), mcp.Required()),
		mcp.WithString("storagePath", mcp.Description("Path where to store the data")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("store_alert_data", alertTool.handleStoreAlertData)))

	fmt.Printf("DEBUG: store_alert_data tool registered successfully\n")

	fmt.Printf("DEBUG: All Alert Collector tools registered successfully\n")

	// Register generate remediation script tool
	s.AddTool(mcp.NewTool("generate_remediation_script",
		mcp.WithDescription("Generate a remediation script based on alert analysis"),
		mcp.WithString("alertId", mcp.Description("Alert ID to generate remediation for"), mcp.Required()),
		mcp.WithString("targetService", mcp.Description("Target service name"), mcp.Required()),
		mcp.WithString("targetNamespace", mcp.Description("Target namespace"), mcp.Required()),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("generate_remediation_script", alertTool.handleGenerateRemediationScript)))

	fmt.Printf("DEBUG: generate_remediation_script tool registered successfully\n")

	// Register query alerts tool for chatbot
	s.AddTool(mcp.NewTool("query_alerts",
		mcp.WithDescription("Query alerts for chatbot interactions"),
		mcp.WithString("timeRange", mcp.Description("Time range (e.g., '3h', '1d', '7d')"), mcp.Required()),
		mcp.WithString("severity", mcp.Description("Severity filter (Critical, High, Medium, Low)")),
		mcp.WithString("service", mcp.Description("Service name filter")),
		mcp.WithString("limit", mcp.Description("Maximum number of results")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("query_alerts", alertTool.handleQueryAlerts)))

	fmt.Printf("DEBUG: query_alerts tool registered successfully\n")

	// Register collect alert data with Jira integration tool
	s.AddTool(mcp.NewTool("collect_alert_data_with_jira",
		mcp.WithDescription("Collect comprehensive data for a target service including pods, events, logs, and related Jira issues"),
		mcp.WithString("targetService", mcp.Description("Name of the target service to collect data for"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace where the target service is running"), mcp.Required()),
		mcp.WithString("collectPods", mcp.Description("Whether to collect pod information (true/false)")),
		mcp.WithString("collectEvents", mcp.Description("Whether to collect events (true/false)")),
		mcp.WithString("collectLogs", mcp.Description("Whether to collect logs (true/false)")),
		mcp.WithString("maxLogLines", mcp.Description("Maximum number of log lines to collect")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("collect_alert_data_with_jira", alertTool.handleCollectAlertDataWithJira)))

	fmt.Printf("DEBUG: collect_alert_data_with_jira tool registered successfully\n")
}
