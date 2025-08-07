package chatbot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tmc/langchaingo/llms"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kagent-dev/tools/internal/telemetry"
	"github.com/kagent-dev/tools/pkg/alerts"
)

// ChatSession represents a user's chat session with context
type ChatSession struct {
	SessionID     string
	LastPodName   string
	LastNamespace string
	LastTimeRange string
	LastIntent    string
	LastAlerts    []map[string]interface{}
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Interactions  []ChatInteraction
}

// ChatInteraction represents a single interaction in the chat
type ChatInteraction struct {
	Timestamp time.Time
	Query     string
	Intent    string
	Response  string
	PodName   string
	Namespace string
}

// SessionManager manages chat sessions
type SessionManager struct {
	sessions map[string]*ChatSession
	mutex    sync.RWMutex
}

// NewSessionManager creates a new session manager
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*ChatSession),
	}
}

// GetOrCreateSession gets an existing session or creates a new one
func (sm *SessionManager) GetOrCreateSession(sessionID string) *ChatSession {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if session, exists := sm.sessions[sessionID]; exists {
		session.UpdatedAt = time.Now()
		return session
	}

	session := &ChatSession{
		SessionID:    sessionID,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Interactions: make([]ChatInteraction, 0),
	}
	sm.sessions[sessionID] = session
	return session
}

// UpdateSessionContext updates the session with new context
func (sm *SessionManager) UpdateSessionContext(sessionID string, podName, namespace, timeRange, intent string, alerts []map[string]interface{}) {
	session := sm.GetOrCreateSession(sessionID)

	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if podName != "" {
		session.LastPodName = podName
	}
	if namespace != "" {
		session.LastNamespace = namespace
	}
	if timeRange != "" {
		session.LastTimeRange = timeRange
	}
	if intent != "" {
		session.LastIntent = intent
	}
	if len(alerts) > 0 {
		session.LastAlerts = alerts
	}
	session.UpdatedAt = time.Now()
}

// AddInteraction adds a new interaction to the session
func (sm *SessionManager) AddInteraction(sessionID, query, intent, response, podName, namespace string) {
	session := sm.GetOrCreateSession(sessionID)

	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	interaction := ChatInteraction{
		Timestamp: time.Now(),
		Query:     query,
		Intent:    intent,
		Response:  response,
		PodName:   podName,
		Namespace: namespace,
	}

	// Keep only last 10 interactions
	if len(session.Interactions) >= 10 {
		session.Interactions = session.Interactions[1:]
	}
	session.Interactions = append(session.Interactions, interaction)
	session.UpdatedAt = time.Now()
}

// GetSessionContext gets the current session context
func (sm *SessionManager) GetSessionContext(sessionID string) *ChatSession {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	if session, exists := sm.sessions[sessionID]; exists {
		return session
	}
	return nil
}

// CleanupOldSessions removes sessions older than 1 hour
func (sm *SessionManager) CleanupOldSessions() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	cutoff := time.Now().Add(-1 * time.Hour)
	for sessionID, session := range sm.sessions {
		if session.UpdatedAt.Before(cutoff) {
			delete(sm.sessions, sessionID)
		}
	}
}

// KAgentChatbotAgent provides intelligent chatbot capabilities for Kubernetes alert management
type KAgentChatbotAgent struct {
	llmModel    llms.Model
	mongoClient *mongo.Client
	kubeconfig  string
	sessionMgr  *SessionManager
	jiraTool    *alerts.JiraIntegrationTool
}

// NewKAgentChatbotAgent creates a new chatbot agent
func NewKAgentChatbotAgent(llmModel llms.Model, mongoClient *mongo.Client, kubeconfig string) *KAgentChatbotAgent {
	return &KAgentChatbotAgent{
		llmModel:    llmModel,
		mongoClient: mongoClient,
		kubeconfig:  kubeconfig,
		sessionMgr:  NewSessionManager(),
	}
}

// NewKAgentChatbotAgentWithJira creates a new chatbot agent with Jira integration
func NewKAgentChatbotAgentWithJira(llmModel llms.Model, mongoClient *mongo.Client, kubeconfig string, jiraConfig alerts.JiraSearchConfig) *KAgentChatbotAgent {
	return &KAgentChatbotAgent{
		llmModel:    llmModel,
		mongoClient: mongoClient,
		kubeconfig:  kubeconfig,
		sessionMgr:  NewSessionManager(),
		jiraTool:    alerts.NewJiraIntegrationTool(jiraConfig),
	}
}

// handleChatbotQuery handles general chatbot queries about alerts and issues
func (a *KAgentChatbotAgent) handleChatbotQuery(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := mcp.ParseString(request, "query", "")
	timeRange := mcp.ParseString(request, "timeRange", "3h")
	limit := mcp.ParseInt(request, "limit", 5)
	sessionID := mcp.ParseString(request, "sessionId", "default")

	if a.llmModel == nil {
		return mcp.NewToolResultError("LLM model not available"), nil
	}

	// Get or create session context
	session := a.sessionMgr.GetOrCreateSession(sessionID)
	fmt.Printf("DEBUG: Session context for %s: LastPodName=%s, LastNamespace=%s\n", sessionID, session.LastPodName, session.LastNamespace)

	// Parse the user query to understand intent with session context
	intent, filters := a.parseQueryIntentWithContext(query, session)
	fmt.Printf("DEBUG: Query intent: %s, filters: %+v\n", intent, filters)

	// For specific intents, handle them specially
	if intent == "log_display" && filters["podName"] != "" {
		fmt.Printf("DEBUG: Handling log_display intent for pod: %s\n", filters["podName"])
		// For log display, we need to query the specific pod data
		collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
		filter := bson.M{"podName": filters["podName"]}
		if filters["namespace"] != "" {
			filter["namespace"] = filters["namespace"]
		}

		var podData []map[string]interface{}
		cursor, err := collection.Find(ctx, filter)
		if err == nil {
			cursor.All(ctx, &podData)
			cursor.Close(ctx)
			fmt.Printf("DEBUG: Found %d pod data entries for log display\n", len(podData))
		} else {
			fmt.Printf("DEBUG: Error querying pod data: %v\n", err)
		}

		// Format log response
		response := a.formatLogDisplayResponse(podData, filters["podName"])
		return mcp.NewToolResultText(response), nil
	}

	if intent == "jira_search" {
		fmt.Printf("DEBUG: Handling jira_search intent\n")
		// Handle Jira search
		if a.jiraTool != nil {
			// Build search query based on the user query
			searchQuery := a.buildJiraSearchQuery(query)

			// Use the actual Jira integration
			if a.jiraTool != nil {
				// Call the Jira search directly with configured values
				searchResponse, err := a.jiraTool.SearchJiraIssues(ctx, searchQuery, 5, 0.05)
				if err == nil {
					// Format the results for display
					response := a.formatJiraSearchResponse(searchResponse)

					// Update session context for Jira search
					a.sessionMgr.UpdateSessionContext(sessionID, filters["podName"], filters["namespace"], timeRange, intent, nil)
					a.sessionMgr.AddInteraction(sessionID, query, intent, response, filters["podName"], filters["namespace"])

					return mcp.NewToolResultText(response), nil
				} else {
					// Error response
					response := fmt.Sprintf("Failed to search Jira issues: %v\n\nSearch query was: '%s'", err, searchQuery)

					// Update session context even for errors
					a.sessionMgr.UpdateSessionContext(sessionID, filters["podName"], filters["namespace"], timeRange, intent, nil)
					a.sessionMgr.AddInteraction(sessionID, query, intent, response, filters["podName"], filters["namespace"])

					return mcp.NewToolResultText(response), nil
				}
			} else {
				// No Jira tool configured
				response := "Jira integration is not configured. Please set up the Jira search service."

				// Update session context
				a.sessionMgr.UpdateSessionContext(sessionID, filters["podName"], filters["namespace"], timeRange, intent, nil)
				a.sessionMgr.AddInteraction(sessionID, query, intent, response, filters["podName"], filters["namespace"])

				return mcp.NewToolResultText(response), nil
			}
		} else {
			// No Jira tool available
			response := "Jira integration is not available. Please configure the Jira search service to find similar issues."

			// Update session context
			a.sessionMgr.UpdateSessionContext(sessionID, filters["podName"], filters["namespace"], timeRange, intent, nil)
			a.sessionMgr.AddInteraction(sessionID, query, intent, response, filters["podName"], filters["namespace"])

			return mcp.NewToolResultText(response), nil
		}
	}

	if intent == "pod_issues" {
		fmt.Printf("DEBUG: Handling pod_issues intent\n")
		// Handle pod issues by calling the pod_status_query tool
		// This will query actual Kubernetes pods, not stored alerts
		namespace := filters["namespace"]
		if namespace == "" {
			namespace = "test-namespace" // Default to test-namespace where we have failing pods
		}

		// Call the pod status query tool
		podStatusRequest := mcp.CallToolRequest{}
		podStatusRequest.Params.Arguments = map[string]interface{}{
			"timeRange": timeRange,
			"namespace": namespace,
			"status":    "Failed",
		}

		result, err := a.handlePodStatusQuery(ctx, podStatusRequest)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to query pod status: %v", err)), nil
		}

		return result, nil
	}

	if intent == "remediation_request" {
		// For remediation, use session context
		if session != nil && session.LastPodName != "" {
			collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
			filter := bson.M{"podName": session.LastPodName}
			if session.LastNamespace != "" {
				filter["namespace"] = session.LastNamespace
			}

			var alertData map[string]interface{}
			err := collection.FindOne(ctx, filter).Decode(&alertData)
			if err == nil {
				remediationScript, err := a.generateRemediationScript(ctx, alertData)
				if err == nil {
					return mcp.NewToolResultText(remediationScript), nil
				}
			}
		}
	}

	// For other intents, use the general query approach
	alerts, err := a.queryAlerts(ctx, timeRange, filters, limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to query alerts: %v", err)), nil
	}
	fmt.Printf("DEBUG: Found %d alerts\n", len(alerts))

	// Update session context
	a.sessionMgr.UpdateSessionContext(sessionID, filters["podName"], filters["namespace"], timeRange, intent, alerts)

	// Generate intelligent response based on intent and data
	response, err := a.generateIntelligentResponseWithContext(ctx, query, intent, alerts, session)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to generate response: %v", err)), nil
	}

	// Add interaction to session
	a.sessionMgr.AddInteraction(sessionID, query, intent, response, filters["podName"], filters["namespace"])

	return mcp.NewToolResultText(response), nil
}

// parseQueryIntent analyzes the user query to understand what they want
func (a *KAgentChatbotAgent) parseQueryIntent(query string) (string, map[string]string) {
	query = strings.ToLower(query)
	filters := make(map[string]string)

	// Determine intent
	var intent string
	switch {
	case strings.Contains(query, "last") || strings.Contains(query, "recent"):
		intent = "recent_alerts"
	case strings.Contains(query, "critical") || strings.Contains(query, "high"):
		intent = "high_severity"
		filters["severity"] = "Critical"
	case strings.Contains(query, "crash") || strings.Contains(query, "pod") || strings.Contains(query, "down") || strings.Contains(query, "failure") || strings.Contains(query, "failures"):
		intent = "pod_issues"
		// Don't filter by specific issue type - let it find all pod failures
		// filters["issueType"] = "PodCrash"  // Removed hardcoded issue type
		// Add phase filter for failed/down pods
		if strings.Contains(query, "down") {
			filters["phase"] = "Failed"
		}
	case strings.Contains(query, "service") || strings.Contains(query, "unavailable"):
		intent = "service_issues"
		filters["issueType"] = "ServiceUnavailable"
	case strings.Contains(query, "resource") || strings.Contains(query, "memory") || strings.Contains(query, "cpu"):
		intent = "resource_issues"
		filters["issueType"] = "ResourceExhaustion"
	case strings.Contains(query, "trend") || strings.Contains(query, "pattern"):
		intent = "trend_analysis"
	case strings.Contains(query, "remediation") || strings.Contains(query, "fix") || strings.Contains(query, "remediation script"):
		intent = "remediation_request"
		// Try to extract pod name from context if available
		if strings.Contains(query, "this") || strings.Contains(query, "pod-1") {
			filters["podName"] = "pod-1-crash"
		}
	default:
		intent = "general_inquiry"
	}

	return intent, filters
}

// parseQueryIntentWithContext analyzes the user query with session context
func (a *KAgentChatbotAgent) parseQueryIntentWithContext(query string, session *ChatSession) (string, map[string]string) {
	query = strings.ToLower(query)
	filters := make(map[string]string)

	// Determine intent
	var intent string
	switch {
	case strings.Contains(query, "last") || strings.Contains(query, "recent"):
		intent = "recent_alerts"
	case strings.Contains(query, "critical") || strings.Contains(query, "high"):
		intent = "high_severity"
		filters["severity"] = "Critical"
	case strings.Contains(query, "jira") || strings.Contains(query, "similar") || strings.Contains(query, "related issues") || strings.Contains(query, "tickets"):
		intent = "jira_search"
		fmt.Printf("DEBUG: Detected jira_search intent for query: %s\n", query)
		// Use session context for pod name
		if session != nil && session.LastPodName != "" {
			filters["podName"] = session.LastPodName
		}
	case strings.Contains(query, "crash") || strings.Contains(query, "pod") || strings.Contains(query, "down") || strings.Contains(query, "failure") || strings.Contains(query, "failures"):
		intent = "pod_issues"
		// Don't filter by specific issue type - let it find all pod failures
		// filters["issueType"] = "PodCrash"  // Removed hardcoded issue type
		// Add phase filter for failed/down pods
		if strings.Contains(query, "down") {
			filters["phase"] = "Failed"
		}
	case strings.Contains(query, "service") || strings.Contains(query, "unavailable"):
		intent = "service_issues"
		filters["issueType"] = "ServiceUnavailable"
	case strings.Contains(query, "resource") || strings.Contains(query, "memory") || strings.Contains(query, "cpu"):
		intent = "resource_issues"
		filters["issueType"] = "ResourceExhaustion"
	case strings.Contains(query, "trend") || strings.Contains(query, "pattern"):
		intent = "trend_analysis"
	case strings.Contains(query, "remediation") || strings.Contains(query, "fix") || strings.Contains(query, "remediation script"):
		intent = "remediation_request"
		// Use session context for pod name
		if session != nil && session.LastPodName != "" {
			filters["podName"] = session.LastPodName
		} else if strings.Contains(query, "this") || strings.Contains(query, "pod-1") {
			filters["podName"] = "pod-1-crash"
		}
	case strings.Contains(query, "logs") || strings.Contains(query, "show logs") || strings.Contains(query, "show me logs"):
		intent = "log_display"
		fmt.Printf("DEBUG: Detected log_display intent for query: %s\n", query)
		// Use session context for pod name
		if session != nil && session.LastPodName != "" {
			filters["podName"] = session.LastPodName
			fmt.Printf("DEBUG: Using session pod name: %s\n", session.LastPodName)
		}
	case strings.Contains(query, "analysis") || strings.Contains(query, "explain"):
		intent = "analysis_display"
	default:
		intent = "general_inquiry"
	}

	// Use session context for smart defaults
	if session != nil {
		if filters["namespace"] == "" && session.LastNamespace != "" {
			filters["namespace"] = session.LastNamespace
		}
		if filters["podName"] == "" && session.LastPodName != "" && (strings.Contains(query, "this") || strings.Contains(query, "it")) {
			filters["podName"] = session.LastPodName
		}
	}

	return intent, filters
}

// queryAlerts retrieves alerts from MongoDB with filtering
func (a *KAgentChatbotAgent) queryAlerts(ctx context.Context, timeRange string, filters map[string]string, limit int) ([]map[string]interface{}, error) {
	if a.mongoClient == nil {
		return nil, fmt.Errorf("MongoDB client not available")
	}

	// Calculate time range
	duration, err := a.parseTimeRange(timeRange)
	if err != nil {
		return nil, err
	}

	// Build filter - try pod_alerts collection first
	filter := bson.M{
		"createdAt": bson.M{"$gte": time.Now().Add(-duration)},
	}

	// Add additional filters
	for key, value := range filters {
		if key == "issueType" {
			filter["issueType"] = value
		} else if key == "severity" {
			filter["severity"] = value
		} else if key == "phase" {
			filter["phase"] = value
		} else if key == "podName" {
			filter["podName"] = value
		} else {
			filter[key] = value
		}
	}

	// Set up options
	opts := options.Find().SetLimit(int64(limit)).SetSort(bson.D{{Key: "createdAt", Value: -1}})

	// Query MongoDB - try pod_alerts collection first, then fallback to alerts
	collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		// Fallback to old alerts collection with different filter structure
		collection = a.mongoClient.Database("kagent-alerts").Collection("alerts")
		filter = bson.M{
			"timestamp": bson.M{"$gte": time.Now().Add(-duration)},
		}
		for key, value := range filters {
			if key == "issueType" {
				filter["analysisResult.dataAnalysis.issueType"] = value
			} else {
				filter[key] = value
			}
		}
		opts = options.Find().SetLimit(int64(limit)).SetSort(bson.D{{Key: "timestamp", Value: -1}})
		cursor, err = collection.Find(ctx, filter, opts)
		if err != nil {
			return nil, err
		}
	}
	defer cursor.Close(ctx)

	// Decode results
	var alerts []map[string]interface{}
	if err := cursor.All(ctx, &alerts); err != nil {
		return nil, err
	}

	return alerts, nil
}

// parseTimeRange converts time range string to duration
func (a *KAgentChatbotAgent) parseTimeRange(timeRange string) (time.Duration, error) {
	switch timeRange {
	case "1h":
		return time.Hour, nil
	case "3h":
		return 3 * time.Hour, nil
	case "6h":
		return 6 * time.Hour, nil
	case "12h":
		return 12 * time.Hour, nil
	case "1d":
		return 24 * time.Hour, nil
	case "7d":
		return 7 * 24 * time.Hour, nil
	case "30d":
		return 30 * 24 * time.Hour, nil
	default:
		return time.ParseDuration(timeRange)
	}
}

// generateIntelligentResponse creates an intelligent response based on the query and data
func (a *KAgentChatbotAgent) generateIntelligentResponse(ctx context.Context, query, intent string, alerts []map[string]interface{}) (string, error) {
	// Handle remediation requests specially
	if intent == "remediation_request" && len(alerts) > 0 {
		// Use the first alert for remediation
		alertData := alerts[0]
		remediation, err := a.generateRemediationScript(ctx, alertData)
		if err != nil {
			return fmt.Sprintf("Failed to generate remediation: %v", err), nil
		}
		return remediation, nil
	}

	// Prepare context for LLM
	contextData := a.prepareContextData(alerts)

	prompt := fmt.Sprintf(`You are a Kubernetes operations expert chatbot. A user asked: "%s"

Based on the following alert data, provide a helpful, actionable response:

Alert Data: %s

Intent: %s
Number of alerts found: %d

Please provide:
1. A clear summary of the situation
2. Key insights from the data
3. Recommended actions if any issues are found
4. Any patterns or trends you notice

Keep the response concise, professional, and actionable.`, query, contextData, intent, len(alerts))

	contents := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: prompt},
			},
		},
	}

	// Add timeout to LLM call to prevent hanging
	llmCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := a.llmModel.GenerateContent(llmCtx, contents, llms.WithModel("gpt-4o-mini"))
	if err != nil {
		// Return a fallback response instead of failing
		return fmt.Sprintf("Analysis completed but LLM response failed: %v\n\nAlert Summary: Found %d alerts in the specified time range.\n\nKey Findings:\n- %d alerts found\n- Intent: %s\n\nPlease try a more specific query or check the logs for more details.", err, len(alerts), len(alerts), intent), nil
	}

	if len(resp.Choices) == 0 {
		return fmt.Sprintf("No LLM response generated.\n\nAlert Summary: Found %d alerts in the specified time range.\n\nKey Findings:\n- %d alerts found\n- Intent: %s", len(alerts), len(alerts), intent), nil
	}

	return resp.Choices[0].Content, nil
}

// generateIntelligentResponseWithContext creates an intelligent response with session context
func (a *KAgentChatbotAgent) generateIntelligentResponseWithContext(ctx context.Context, query, intent string, alerts []map[string]interface{}, session *ChatSession) (string, error) {
	// Handle remediation requests specially
	if intent == "remediation_request" && len(alerts) > 0 {
		// Use the first alert for remediation
		alertData := alerts[0]
		remediation, err := a.generateRemediationScript(ctx, alertData)
		if err != nil {
			return fmt.Sprintf("Failed to generate remediation: %v", err), nil
		}
		return remediation, nil
	}

	// Prepare context for LLM with session information
	contextData := a.prepareContextData(alerts)
	sessionContext := a.prepareSessionContext(session)

	prompt := fmt.Sprintf(`You are a Kubernetes operations expert chatbot. A user asked: "%s"

Session Context: %s

Based on the following alert data, provide a helpful, actionable response with proactive suggestions and rich formatting:

Alert Data: %s

Intent: %s
Number of alerts found: %d

Please provide a well-formatted response with:

**ANALYSIS:**
🔍 [Your main analysis and insights with clear sections]

**KEY FINDINGS:**
📊 [Key metrics, patterns, or trends you notice]

**RECOMMENDATIONS:**
⚡ [Immediate actions or recommendations]

**SUGGESTIONS:**
💡 [2-3 specific next steps the user might want to take]

Use rich formatting with:
- Emojis for visual appeal (🔍, 📊, ⚡, 💡, 🚨, ✅, ❌, etc.)
- Clear section headers with bold formatting
- Bullet points for easy scanning
- Specific pod names in **bold**
- Severity levels with appropriate emojis (🟢 Low, 🟡 Medium, 🔴 High, ⚫ Critical)

Examples of good proactive suggestions:
- "🔍 Would you like me to show the logs for **pod-1-crash**?"
- "⚡ Should I generate a remediation script for this issue?"
- "📊 Would you like to see the detailed analysis for this pod?"
- "🔍 Should I check for similar issues in other namespaces?"
- "⏰ Would you like me to monitor this pod for the next hour?"

Be specific, contextual, and use rich formatting to make the response engaging and easy to read.`, query, sessionContext, contextData, intent, len(alerts))

	contents := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: prompt},
			},
		},
	}

	// Add timeout to LLM call to prevent hanging
	llmCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := a.llmModel.GenerateContent(llmCtx, contents, llms.WithModel("gpt-4o-mini"))
	if err != nil {
		// Return a fallback response instead of failing
		return fmt.Sprintf("Analysis completed but LLM response failed: %v\n\nAlert Summary: Found %d alerts in the specified time range.\n\nKey Findings:\n- %d alerts found\n- Intent: %s\n\nPlease try a more specific query or check the logs for more details.", err, len(alerts), len(alerts), intent), nil
	}

	if len(resp.Choices) == 0 {
		return fmt.Sprintf("No LLM response generated.\n\nAlert Summary: Found %d alerts in the specified time range.\n\nKey Findings:\n- %d alerts found\n- Intent: %s", len(alerts), len(alerts), intent), nil
	}

	return resp.Choices[0].Content, nil
}

// prepareSessionContext formats session data for LLM context
func (a *KAgentChatbotAgent) prepareSessionContext(session *ChatSession) string {
	if session == nil {
		return "No previous session context available."
	}

	var context strings.Builder
	context.WriteString(fmt.Sprintf("Session ID: %s\n", session.SessionID))

	if session.LastPodName != "" {
		context.WriteString(fmt.Sprintf("Last discussed pod: %s\n", session.LastPodName))
	}
	if session.LastNamespace != "" {
		context.WriteString(fmt.Sprintf("Last namespace: %s\n", session.LastNamespace))
	}
	if session.LastTimeRange != "" {
		context.WriteString(fmt.Sprintf("Last time range: %s\n", session.LastTimeRange))
	}
	if session.LastIntent != "" {
		context.WriteString(fmt.Sprintf("Last intent: %s\n", session.LastIntent))
	}

	if len(session.Interactions) > 0 {
		context.WriteString("Recent interactions:\n")
		for i, interaction := range session.Interactions {
			if i >= 3 { // Show only last 3 interactions
				break
			}
			context.WriteString(fmt.Sprintf("- %s: %s (intent: %s)\n",
				interaction.Timestamp.Format("15:04:05"),
				interaction.Query,
				interaction.Intent))
		}
	}

	return context.String()
}

// prepareContextData formats alert data for LLM context
func (a *KAgentChatbotAgent) prepareContextData(alerts []map[string]interface{}) string {
	if len(alerts) == 0 {
		return "No alerts found in the specified time range."
	}

	var summaries []string
	for _, alert := range alerts {
		// Extract timestamp information
		createdAt := "Unknown"
		updatedAt := "Unknown"
		collectedAt := "Unknown"

		if createdAtVal, ok := alert["createdAt"]; ok {
			if t, ok := createdAtVal.(time.Time); ok {
				createdAt = t.Format("2006-01-02 15:04:05")
			}
		}
		if updatedAtVal, ok := alert["updatedAt"]; ok {
			if t, ok := updatedAtVal.(time.Time); ok {
				updatedAt = t.Format("2006-01-02 15:04:05")
			}
		}
		if collectedAtVal, ok := alert["collectedAt"]; ok {
			if t, ok := collectedAtVal.(time.Time); ok {
				collectedAt = t.Format("2006-01-02 15:04:05")
			}
		}

		// Extract metadata counts
		eventCount := 0
		if eventCountVal, ok := alert["eventCount"]; ok {
			if count, ok := eventCountVal.(int); ok {
				eventCount = count
			}
		}

		// Handle both old and new alert structures
		alertID := a.getNestedString(alert, "alertId")
		service := a.getNestedString(alert, "targetService")
		if service == "Unknown" {
			service = a.getNestedString(alert, "service")
		}
		namespace := a.getNestedString(alert, "targetNamespace")
		if namespace == "Unknown" {
			namespace = a.getNestedString(alert, "namespace")
		}
		severity := a.getNestedString(alert, "severity")
		status := a.getNestedString(alert, "status")

		// Get pod-specific information for new structure
		podName := a.getNestedString(alert, "podName")
		issueType := a.getNestedString(alert, "issueType")

		// Get summary from analysis result or use issue type
		summary := "Unknown"
		if analysisResult, ok := alert["analysisResult"].(map[string]interface{}); ok {
			summary = a.getNestedString(analysisResult, "summary")
		} else if issueType != "Unknown" {
			summary = fmt.Sprintf("Pod issue: %s", issueType)
		}

		summaryText := fmt.Sprintf("Alert ID: %s, Pod: %s, Service: %s, Namespace: %s, Issue Type: %s, Severity: %s, Status: %s, Created: %s, Updated: %s, Collected: %s, Events: %d, Summary: %s",
			alertID,
			podName,
			service,
			namespace,
			issueType,
			severity,
			status,
			createdAt,
			updatedAt,
			collectedAt,
			eventCount,
			summary)
		summaries = append(summaries, summaryText)
	}

	return strings.Join(summaries, "\n")
}

// getNestedString safely extracts nested string values
func (a *KAgentChatbotAgent) getNestedString(data map[string]interface{}, path string) string {
	keys := strings.Split(path, ".")
	current := data

	for _, key := range keys {
		if val, ok := current[key]; ok {
			if nested, ok := val.(map[string]interface{}); ok {
				current = nested
			} else if str, ok := val.(string); ok {
				return str
			} else if timeVal, ok := val.(time.Time); ok {
				return timeVal.Format("2006-01-02 15:04:05")
			} else if primitiveTime, ok := val.(primitive.DateTime); ok {
				return time.Unix(int64(primitiveTime)/1000, 0).Format("2006-01-02 15:04:05")
			} else {
				// Convert any other type to string
				return fmt.Sprintf("%v", val)
			}
		}
	}
	return "Unknown"
}

// handleGetRemediation handles requests for remediation scripts
func (a *KAgentChatbotAgent) handleGetRemediation(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	alertID := mcp.ParseString(request, "alertId", "")
	service := mcp.ParseString(request, "service", "")
	namespace := mcp.ParseString(request, "namespace", "")
	sessionID := mcp.ParseString(request, "sessionId", "default")

	if a.llmModel == nil {
		return mcp.NewToolResultError("LLM model not available"), nil
	}

	// Get session context
	session := a.sessionMgr.GetSessionContext(sessionID)

	// If no specific alert data provided, try to use session context
	if alertID == "" && session != nil && session.LastPodName != "" {
		// Query MongoDB for the pod data
		collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
		filter := bson.M{"podName": session.LastPodName}
		if session.LastNamespace != "" {
			filter["namespace"] = session.LastNamespace
		}

		var alertData map[string]interface{}
		err := collection.FindOne(ctx, filter).Decode(&alertData)
		if err == nil {
			// Generate remediation script using session context
			remediationScript, err := a.generateRemediationScript(ctx, alertData)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to generate remediation: %v", err)), nil
			}
			return mcp.NewToolResultText(remediationScript), nil
		} else {
			// Log the error for debugging
			fmt.Printf("DEBUG: Failed to find pod data for session: %s, pod: %s, error: %v\n", sessionID, session.LastPodName, err)
		}
	}

	// Fallback to original method
	alertData, err := a.getAlertData(ctx, alertID, service, namespace)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get alert data: %v", err)), nil
	}

	// Generate remediation script
	remediationScript, err := a.generateRemediationScript(ctx, alertData)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to generate remediation: %v", err)), nil
	}

	return mcp.NewToolResultText(remediationScript), nil
}

// getAlertData retrieves specific alert data from MongoDB
func (a *KAgentChatbotAgent) getAlertData(ctx context.Context, alertID, service, namespace string) (map[string]interface{}, error) {
	if a.mongoClient == nil {
		return nil, fmt.Errorf("MongoDB client not available")
	}

	// First try to get from alerts collection (detailed alerts)
	collection := a.mongoClient.Database("kagent-alerts").Collection("alerts")
	filter := bson.M{
		"alertId":         alertID,
		"targetService":   service,
		"targetNamespace": namespace,
	}

	var alertData map[string]interface{}
	err := collection.FindOne(ctx, filter).Decode(&alertData)
	if err == nil {
		return alertData, nil
	}

	// If not found in alerts collection, try pod_alerts collection
	collection = a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
	filter = bson.M{
		"alertId": alertID,
	}

	err = collection.FindOne(ctx, filter).Decode(&alertData)
	if err != nil {
		return nil, fmt.Errorf("alert not found in either alerts or pod_alerts collections")
	}

	return alertData, nil
}

// generateRemediationScript creates a remediation script based on alert data
func (a *KAgentChatbotAgent) generateRemediationScript(ctx context.Context, alertData map[string]interface{}) (string, error) {
	// Check if this is a detailed alert with analysis result
	var analysisResult map[string]interface{}
	var service, namespace, severity, issueType, summary string
	var logAnalysis, eventAnalysis string
	var affectedResources interface{}

	if analysisResultVal, ok := alertData["analysisResult"]; ok {
		analysisResult = analysisResultVal.(map[string]interface{})
		service = a.getNestedString(alertData, "targetService")
		namespace = a.getNestedString(alertData, "targetNamespace")
		severity = a.getNestedString(alertData, "severity")
		issueType = a.getNestedString(analysisResult, "dataAnalysis.issueType")
		summary = a.getNestedString(analysisResult, "summary")
		logAnalysis = a.getNestedString(analysisResult, "dataAnalysis.logAnalysis")
		eventAnalysis = a.getNestedString(analysisResult, "dataAnalysis.eventAnalysis")
		affectedResources = a.getNestedInterface(analysisResult, "dataAnalysis.affectedResources")
	} else {
		// This is a pod alert with basic structure
		service = a.getNestedString(alertData, "service")
		namespace = a.getNestedString(alertData, "namespace")
		severity = a.getNestedString(alertData, "severity")
		issueType = a.getNestedString(alertData, "issueType")
		summary = fmt.Sprintf("Pod %s in namespace %s has %s issue",
			a.getNestedString(alertData, "podName"),
			namespace,
			issueType)
		logAnalysis = "Basic pod alert - detailed analysis not available"
		eventAnalysis = "Basic pod alert - detailed analysis not available"
		affectedResources = []string{a.getNestedString(alertData, "podName")}
	}

	prompt := fmt.Sprintf(`Generate a practical remediation script for this Kubernetes issue:

Alert Details:
- Service: %s
- Namespace: %s
- Severity: %s
- Issue Type: %s
- Summary: %s

Analysis:
- Log Analysis: %s
- Event Analysis: %s
- Affected Resources: %v

Please generate a remediation script that:
1. Addresses the specific issue type
2. Includes safety checks and validation
3. Provides rollback instructions
4. Includes monitoring and verification steps
5. Uses kubectl commands where appropriate

Format the script with clear comments and error handling.`,
		service,
		namespace,
		severity,
		issueType,
		summary,
		logAnalysis,
		eventAnalysis,
		affectedResources)

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
		return fmt.Sprintf("Failed to generate remediation script: %v", err), nil
	}

	if len(resp.Choices) == 0 {
		return "No remediation script generated", nil
	}

	return resp.Choices[0].Content, nil
}

// getNestedInterface safely extracts nested interface values
func (a *KAgentChatbotAgent) getNestedInterface(data map[string]interface{}, path string) interface{} {
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

// handlePodStatusQuery handles queries about pod status and failures
func (a *KAgentChatbotAgent) handlePodStatusQuery(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	timeRange := mcp.ParseString(request, "timeRange", "2h")
	namespace := mcp.ParseString(request, "namespace", "")
	status := mcp.ParseString(request, "status", "Failed") // Default to failed pods

	if a.mongoClient == nil {
		return mcp.NewToolResultError("MongoDB client not available"), nil
	}

	// Parse time range
	duration, err := a.parseTimeRange(timeRange)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid time range: %v", err)), nil
	}

	// Build filter for pod status - look for both phase and issueType
	filter := bson.M{
		"createdAt": bson.M{"$gte": time.Now().Add(-duration)},
	}

	// If status is "Failed", look for various failure conditions
	if status == "Failed" {
		// Look for pods with failing issue types or failed phase
		filter["$or"] = []bson.M{
			{"phase": "Failed"},
			{"issueType": bson.M{"$in": []string{"NotReady", "CrashLoop", "Error", "Pending"}}},
		}
	} else {
		// For other status types, use the original phase filter
		filter["phase"] = status
	}

	if namespace != "" {
		filter["namespace"] = namespace
	}

	// Query pod_alerts collection
	collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to query pod status: %v", err)), nil
	}
	defer cursor.Close(ctx)

	var pods []map[string]interface{}
	if err := cursor.All(ctx, &pods); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to decode pod data: %v", err)), nil
	}

	// Format response
	response := a.formatPodStatusResponse(pods, timeRange, status)
	return mcp.NewToolResultText(response), nil
}

// handleLogDisplay handles requests to show logs from specific pods
func (a *KAgentChatbotAgent) handleLogDisplay(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := mcp.ParseString(request, "podName", "")
	namespace := mcp.ParseString(request, "namespace", "")
	limit := mcp.ParseInt(request, "limit", 50)

	fmt.Printf("DEBUG: handleLogDisplay called for pod: %s, namespace: %s, limit: %d\n", podName, namespace, limit)

	if a.mongoClient == nil {
		fmt.Printf("DEBUG: MongoDB client is nil\n")
		return mcp.NewToolResultError("MongoDB client not available"), nil
	}

	if podName == "" {
		fmt.Printf("DEBUG: Pod name is empty\n")
		return mcp.NewToolResultError("Pod name is required"), nil
	}

	// Build filter
	filter := bson.M{"podName": podName}
	if namespace != "" {
		filter["namespace"] = namespace
	}

	fmt.Printf("DEBUG: Querying with filter: %+v\n", filter)

	// Query pod_alerts collection
	collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
	opts := options.Find().SetLimit(int64(limit)).SetSort(bson.D{{Key: "createdAt", Value: -1}})

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		fmt.Printf("DEBUG: Failed to query pod logs: %v\n", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to query pod logs: %v", err)), nil
	}
	defer cursor.Close(ctx)

	var podData []map[string]interface{}
	if err := cursor.All(ctx, &podData); err != nil {
		fmt.Printf("DEBUG: Failed to decode pod data: %v\n", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to decode pod data: %v", err)), nil
	}

	fmt.Printf("DEBUG: Found %d pod data entries\n", len(podData))

	// Format log response
	response := a.formatLogDisplayResponse(podData, podName)
	fmt.Printf("DEBUG: Formatted response length: %d\n", len(response))
	return mcp.NewToolResultText(response), nil
}

// handleAnalysisDisplay handles requests to show analysis results
func (a *KAgentChatbotAgent) handleAnalysisDisplay(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := mcp.ParseString(request, "podName", "")
	namespace := mcp.ParseString(request, "namespace", "")
	timeRange := mcp.ParseString(request, "timeRange", "24h")

	if a.mongoClient == nil {
		return mcp.NewToolResultError("MongoDB client not available"), nil
	}

	// Parse time range
	duration, err := a.parseTimeRange(timeRange)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid time range: %v", err)), nil
	}

	// Build filter
	filter := bson.M{
		"createdAt": bson.M{"$gte": time.Now().Add(-duration)},
		"status":    "Analyzed",
	}

	if podName != "" {
		filter["podName"] = podName
	}
	if namespace != "" {
		filter["namespace"] = namespace
	}

	// Query pod_alerts collection
	collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
	opts := options.Find().SetSort(bson.D{{Key: "analyzedAt", Value: -1}})

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to query analysis data: %v", err)), nil
	}
	defer cursor.Close(ctx)

	var analysisData []map[string]interface{}
	if err := cursor.All(ctx, &analysisData); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to decode analysis data: %v", err)), nil
	}

	// Format analysis response
	response := a.formatAnalysisDisplayResponse(analysisData, timeRange)
	return mcp.NewToolResultText(response), nil
}

// handleEnhancedRemediation provides enhanced remediation assistance
func (a *KAgentChatbotAgent) handleEnhancedRemediation(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	podName := mcp.ParseString(request, "podName", "")
	namespace := mcp.ParseString(request, "namespace", "")
	issueType := mcp.ParseString(request, "issueType", "")

	if a.llmModel == nil {
		return mcp.NewToolResultError("LLM model not available"), nil
	}

	// Get pod data for context
	var podData map[string]interface{}
	if podName != "" {
		collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")
		filter := bson.M{"podName": podName}
		if namespace != "" {
			filter["namespace"] = namespace
		}

		err := collection.FindOne(ctx, filter).Decode(&podData)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get pod data: %v", err)), nil
		}
	}

	// Generate enhanced remediation
	remediation, err := a.generateEnhancedRemediation(ctx, podData, issueType)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to generate remediation: %v", err)), nil
	}

	return mcp.NewToolResultText(remediation), nil
}

// formatPodStatusResponse formats the response for pod status queries
func (a *KAgentChatbotAgent) formatPodStatusResponse(pods []map[string]interface{}, timeRange, status string) string {
	if len(pods) == 0 {
		return fmt.Sprintf("🔍 No %s pods found in the last %s.", status, timeRange)
	}

	var response strings.Builder
	response.WriteString(fmt.Sprintf("🚨 Found %d %s pods in the last %s:\n\n", len(pods), status, timeRange))

	for i, pod := range pods {
		podName := a.getNestedString(pod, "podName")
		namespace := a.getNestedString(pod, "namespace")
		phase := a.getNestedString(pod, "phase")
		issueType := a.getNestedString(pod, "issueType")
		severity := a.getNestedString(pod, "severity")
		createdAt := a.getNestedString(pod, "createdAt")
		reason := a.getNestedString(pod, "reason")

		// Add severity emoji
		severityEmoji := "🟡"
		switch strings.ToLower(severity) {
		case "low":
			severityEmoji = "🟢"
		case "medium":
			severityEmoji = "🟡"
		case "high":
			severityEmoji = "🔴"
		case "critical":
			severityEmoji = "⚫"
		}

		response.WriteString(fmt.Sprintf("%d. **Pod: %s** %s\n", i+1, podName, severityEmoji))
		response.WriteString(fmt.Sprintf("   📁 Namespace: %s\n", namespace))
		response.WriteString(fmt.Sprintf("   🔄 Phase: %s\n", phase))
		response.WriteString(fmt.Sprintf("   ⚠️  Issue Type: %s\n", issueType))
		response.WriteString(fmt.Sprintf("   %s Severity: %s\n", severityEmoji, severity))
		if reason != "" {
			response.WriteString(fmt.Sprintf("   🔍 Reason: %s\n", reason))
		}
		response.WriteString(fmt.Sprintf("   ⏰ Detected: %s\n", createdAt))
		response.WriteString("\n")
	}

	// Add proactive suggestions with rich formatting
	response.WriteString("\n**💡 SUGGESTIONS:**\n")
	if len(pods) > 0 {
		firstPod := a.getNestedString(pods[0], "podName")
		response.WriteString(fmt.Sprintf("🔍 Would you like me to show the logs for **%s**?\n", firstPod))
		response.WriteString(fmt.Sprintf("⚡ Should I generate a remediation script for **%s**?\n", firstPod))
		response.WriteString(fmt.Sprintf("📊 Would you like to see the detailed analysis for **%s**?\n", firstPod))
	}
	if len(pods) > 1 {
		response.WriteString("🔍 Should I check for similar issues in other namespaces?\n")
	}
	response.WriteString("⏰ Would you like me to monitor these pods for the next hour?\n")

	return response.String()
}

// formatLogDisplayResponse formats the response for log display queries
func (a *KAgentChatbotAgent) formatLogDisplayResponse(podData []map[string]interface{}, podName string) string {
	fmt.Printf("DEBUG: formatLogDisplayResponse called with %d pod data entries\n", len(podData))

	if len(podData) == 0 {
		fmt.Printf("DEBUG: No pod data found\n")
		return fmt.Sprintf("🔍 No data found for pod: **%s**", podName)
	}

	var response strings.Builder
	response.WriteString(fmt.Sprintf("📋 **Logs for pod: %s**\n\n", podName))

	for i, pod := range podData {
		fmt.Printf("DEBUG: Processing pod data entry %d\n", i)

		namespace := a.getNestedString(pod, "namespace")
		phase := a.getNestedString(pod, "phase")
		issueType := a.getNestedString(pod, "issueType")
		collectedAt := a.getNestedString(pod, "collectedAt")

		fmt.Printf("DEBUG: Pod details - namespace: %s, phase: %s, issueType: %s, collectedAt: %s\n", namespace, phase, issueType, collectedAt)

		response.WriteString(fmt.Sprintf("**📊 Pod Details:**\n"))
		response.WriteString(fmt.Sprintf("📁 Namespace: %s\n", namespace))
		response.WriteString(fmt.Sprintf("🔄 Phase: %s\n", phase))
		response.WriteString(fmt.Sprintf("⚠️  Issue Type: %s\n", issueType))
		response.WriteString(fmt.Sprintf("⏰ Collected At: %s\n\n", collectedAt))

		// Display container logs
		if containerLogs, ok := pod["containerLogs"].(map[string]interface{}); ok {
			fmt.Printf("DEBUG: Found containerLogs map with %d containers\n", len(containerLogs))
			response.WriteString("**📝 Container Logs:**\n")
			for container, logs := range containerLogs {
				fmt.Printf("DEBUG: Processing container: %s\n", container)
				if logLines, ok := logs.([]interface{}); ok {
					fmt.Printf("DEBUG: Found %d log lines as []interface{}\n", len(logLines))
					response.WriteString(fmt.Sprintf("\n*🐳 Container: %s*\n", container))
					for _, log := range logLines {
						if logStr, ok := log.(string); ok {
							response.WriteString(fmt.Sprintf("  %s\n", logStr))
						}
					}
				} else if logLines, ok := logs.([]string); ok {
					fmt.Printf("DEBUG: Found %d log lines as []string\n", len(logLines))
					response.WriteString(fmt.Sprintf("\n*🐳 Container: %s*\n", container))
					for _, log := range logLines {
						response.WriteString(fmt.Sprintf("  %s\n", log))
					}
				} else if primitiveArray, ok := logs.(primitive.A); ok {
					fmt.Printf("DEBUG: Found %d log lines as primitive.A\n", len(primitiveArray))
					response.WriteString(fmt.Sprintf("\n*🐳 Container: %s*\n", container))
					for _, log := range primitiveArray {
						if logStr, ok := log.(string); ok {
							response.WriteString(fmt.Sprintf("  %s\n", logStr))
						}
					}
				} else {
					fmt.Printf("DEBUG: Logs type is %T, value: %+v\n", logs, logs)
				}
			}
		} else {
			fmt.Printf("DEBUG: containerLogs not found or not a map, type: %T\n", pod["containerLogs"])
		}

		// Display pod conditions
		if podConditions, ok := pod["podConditions"].([]interface{}); ok {
			response.WriteString("\n**🔍 Pod Conditions:**\n")
			for _, condition := range podConditions {
				if cond, ok := condition.(map[string]interface{}); ok {
					condType := a.getNestedString(cond, "type")
					status := a.getNestedString(cond, "status")
					reason := a.getNestedString(cond, "reason")
					message := a.getNestedString(cond, "message")

					// Add status emoji
					statusEmoji := "🟡"
					if status == "True" {
						statusEmoji = "✅"
					} else if status == "False" {
						statusEmoji = "❌"
					}

					response.WriteString(fmt.Sprintf("%s %s: %s", statusEmoji, condType, status))
					if reason != "" {
						response.WriteString(fmt.Sprintf(" (🔍 Reason: %s)", reason))
					}
					if message != "" {
						response.WriteString(fmt.Sprintf(" - %s", message))
					}
					response.WriteString("\n")
				}
			}
		}
	}

	// Add proactive suggestions with rich formatting
	response.WriteString("\n**💡 SUGGESTIONS:**\n")
	response.WriteString(fmt.Sprintf("⚡ Should I generate a remediation script for **%s**?\n", podName))
	response.WriteString(fmt.Sprintf("📊 Would you like to see the detailed analysis for **%s**?\n", podName))
	response.WriteString("🔍 Should I check for similar issues in other namespaces?\n")
	response.WriteString("⏰ Would you like me to monitor this pod for the next hour?\n")

	return response.String()
}

// formatAnalysisDisplayResponse formats the response for analysis display queries
func (a *KAgentChatbotAgent) formatAnalysisDisplayResponse(analysisData []map[string]interface{}, timeRange string) string {
	if len(analysisData) == 0 {
		return fmt.Sprintf("No analyzed data found in the last %s.", timeRange)
	}

	var response strings.Builder
	response.WriteString(fmt.Sprintf("**Analysis Results (Last %s):**\n\n", timeRange))

	for i, data := range analysisData {
		podName := a.getNestedString(data, "podName")
		namespace := a.getNestedString(data, "namespace")
		issueType := a.getNestedString(data, "issueType")
		severity := a.getNestedString(data, "severity")
		analyzedAt := a.getNestedString(data, "analyzedAt")

		response.WriteString(fmt.Sprintf("%d. **Pod: %s**\n", i+1, podName))
		response.WriteString(fmt.Sprintf("   - Namespace: %s\n", namespace))
		response.WriteString(fmt.Sprintf("   - Issue Type: %s\n", issueType))
		response.WriteString(fmt.Sprintf("   - Severity: %s\n", severity))
		response.WriteString(fmt.Sprintf("   - Analyzed At: %s\n", analyzedAt))

		// Display analysis result
		if analysisResult, ok := data["analysisResult"].(map[string]interface{}); ok {
			response.WriteString("\n   **Analysis:**\n")

			if summary := a.getNestedString(analysisResult, "summary"); summary != "" {
				response.WriteString(fmt.Sprintf("   - Summary: %s\n", summary))
			}

			if dataAnalysis, ok := analysisResult["dataAnalysis"].(map[string]interface{}); ok {
				if logAnalysis := a.getNestedString(dataAnalysis, "logAnalysis"); logAnalysis != "" {
					response.WriteString(fmt.Sprintf("   - Log Analysis: %s\n", logAnalysis))
				}
				if eventAnalysis := a.getNestedString(dataAnalysis, "eventAnalysis"); eventAnalysis != "" {
					response.WriteString(fmt.Sprintf("   - Event Analysis: %s\n", eventAnalysis))
				}
				if dataQuality := a.getNestedString(dataAnalysis, "dataQuality"); dataQuality != "" {
					response.WriteString(fmt.Sprintf("   - Data Quality: %s\n", dataQuality))
				}
			}
		}

		response.WriteString("\n")
	}

	// Add proactive suggestions
	response.WriteString("\n**SUGGESTIONS:**\n")
	if len(analysisData) > 0 {
		firstPod := a.getNestedString(analysisData[0], "podName")
		response.WriteString(fmt.Sprintf("- Would you like me to show the logs for **%s**?\n", firstPod))
		response.WriteString(fmt.Sprintf("- Should I generate a remediation script for **%s**?\n", firstPod))
	}
	response.WriteString("- Should I check for similar issues in other namespaces?\n")
	response.WriteString("- Would you like me to monitor these pods for the next hour?\n")

	return response.String()
}

// generateEnhancedRemediation generates enhanced remediation with more context
func (a *KAgentChatbotAgent) generateEnhancedRemediation(ctx context.Context, podData map[string]interface{}, issueType string) (string, error) {
	var prompt strings.Builder
	prompt.WriteString("You are a Kubernetes operations expert. Generate a comprehensive remediation plan for the following issue:\n\n")

	if podData != nil {
		podName := a.getNestedString(podData, "podName")
		namespace := a.getNestedString(podData, "namespace")
		phase := a.getNestedString(podData, "phase")
		severity := a.getNestedString(podData, "severity")
		issueType = a.getNestedString(podData, "issueType")

		prompt.WriteString(fmt.Sprintf("**Pod Information:**\n"))
		prompt.WriteString(fmt.Sprintf("- Pod Name: %s\n", podName))
		prompt.WriteString(fmt.Sprintf("- Namespace: %s\n", namespace))
		prompt.WriteString(fmt.Sprintf("- Phase: %s\n", phase))
		prompt.WriteString(fmt.Sprintf("- Severity: %s\n", severity))
		prompt.WriteString(fmt.Sprintf("- Issue Type: %s\n\n", issueType))

		// Add container logs if available
		if containerLogs, ok := podData["containerLogs"].(map[string]interface{}); ok {
			prompt.WriteString("**Recent Logs:**\n")
			for container, logs := range containerLogs {
				if logLines, ok := logs.([]string); ok {
					prompt.WriteString(fmt.Sprintf("\nContainer: %s\n", container))
					for _, log := range logLines {
						prompt.WriteString(fmt.Sprintf("  %s\n", log))
					}
				}
			}
			prompt.WriteString("\n")
		}

		// Add analysis result if available
		if analysisResult, ok := podData["analysisResult"].(map[string]interface{}); ok {
			prompt.WriteString("**Analysis Results:**\n")
			if summary := a.getNestedString(analysisResult, "summary"); summary != "" {
				prompt.WriteString(fmt.Sprintf("- Summary: %s\n", summary))
			}
			if dataAnalysis, ok := analysisResult["dataAnalysis"].(map[string]interface{}); ok {
				if logAnalysis := a.getNestedString(dataAnalysis, "logAnalysis"); logAnalysis != "" {
					prompt.WriteString(fmt.Sprintf("- Log Analysis: %s\n", logAnalysis))
				}
				if eventAnalysis := a.getNestedString(dataAnalysis, "eventAnalysis"); eventAnalysis != "" {
					prompt.WriteString(fmt.Sprintf("- Event Analysis: %s\n", eventAnalysis))
				}
			}
			prompt.WriteString("\n")
		}
	} else if issueType != "" {
		prompt.WriteString(fmt.Sprintf("**Issue Type:** %s\n\n", issueType))
	}

	prompt.WriteString("Please provide a comprehensive remediation plan that includes:\n")
	prompt.WriteString("1. **Immediate Actions** - Steps to resolve the current issue\n")
	prompt.WriteString("2. **Root Cause Analysis** - What caused this issue\n")
	prompt.WriteString("3. **Prevention Measures** - How to prevent similar issues\n")
	prompt.WriteString("4. **Monitoring & Verification** - How to verify the fix worked\n")
	prompt.WriteString("5. **Rollback Plan** - How to rollback if needed\n")
	prompt.WriteString("6. **Kubectl Commands** - Specific commands to execute\n\n")
	prompt.WriteString("Format your response with clear sections and actionable steps.")

	contents := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: prompt.String()},
			},
		},
	}

	resp, err := a.llmModel.GenerateContent(ctx, contents, llms.WithModel("gpt-4o-mini"))
	if err != nil {
		return fmt.Sprintf("Failed to generate enhanced remediation: %v", err), nil
	}

	if len(resp.Choices) == 0 {
		return "No remediation plan generated", nil
	}

	return resp.Choices[0].Content, nil
}

// buildJiraSearchQuery builds a search query for Jira based on session context and alerts
func (a *KAgentChatbotAgent) buildJiraSearchQuery(query string) string {
	// Extract pod name directly from the query
	podName := a.extractPodNameFromQuery(query)

	if podName != "" {
		fmt.Printf("DEBUG: Extracted pod name from query: '%s' -> '%s'\n", query, podName)

		// Clean the pod name by removing hyphens, underscores, and other separators
		// Replace them with spaces to create a more natural search query
		cleanedName := strings.ReplaceAll(podName, "-", " ")
		cleanedName = strings.ReplaceAll(cleanedName, "_", " ")
		cleanedName = strings.ReplaceAll(cleanedName, ".", " ")

		// Remove extra spaces and trim
		cleanedName = strings.Join(strings.Fields(cleanedName), " ")

		// Add some context keywords
		searchQuery := fmt.Sprintf("%s kubernetes pod issue", cleanedName)
		fmt.Printf("DEBUG: Built Jira search query: '%s' from pod name: '%s'\n", searchQuery, podName)
		return searchQuery
	} else {
		// Fallback to generic terms if no pod name found
		fmt.Printf("DEBUG: No pod name extracted from query: '%s', using generic query\n", query)
		return "pod kubernetes k8s"
	}
}

// extractPodNameFromQuery tries to extract a pod name from the user query
func (a *KAgentChatbotAgent) extractPodNameFromQuery(query string) string {
	query = strings.ToLower(query)

	// Common pod name patterns
	patterns := []string{
		"spire postgres",
		"spire-postgres",
		"memory crash pod",
		"memory-crash-pod",
		"network failure pod",
		"network-failure-pod",
		"config error pod",
		"config-error-pod",
		"dependency failure pod",
		"dependency-failure-pod",
		"permission error pod",
		"permission-error-pod",
	}

	for _, pattern := range patterns {
		if strings.Contains(query, pattern) {
			// Convert to standard format (with hyphens)
			podName := strings.ReplaceAll(pattern, " ", "-")
			fmt.Printf("DEBUG: Extracted pod name from query: '%s' -> '%s'\n", pattern, podName)
			return podName
		}
	}

	return ""
}

// formatJiraSearchResponse formats Jira search results for chat display with simplified metadata
func (a *KAgentChatbotAgent) formatJiraSearchResponse(searchResponse *alerts.JiraSearchResponse) string {
	if searchResponse == nil || len(searchResponse.Results) == 0 {
		return "No similar Jira issues found."
	}

	var response strings.Builder
	response.WriteString(fmt.Sprintf("🔍 **Found %d similar Jira issues** (Query: \"%s\")\n\n", len(searchResponse.Results), searchResponse.Query))

	for _, result := range searchResponse.Results {
		response.WriteString(fmt.Sprintf("🎯 **%s** - %s\n", result.IssueKey, result.Summary))
		response.WriteString(fmt.Sprintf("   📋 Type: %s | Status: %s | Priority: %s\n", result.IssueType, result.Status, result.Priority))

		if result.Assignee != "" && result.Assignee != "Unassigned" {
			response.WriteString(fmt.Sprintf("   👤 Assignee: %s\n", result.Assignee))
		}

		if len(result.Components) > 0 {
			response.WriteString(fmt.Sprintf("   🔧 Components: %s\n", strings.Join(result.Components, ", ")))
		}

		if result.Description != "" {
			// Truncate description if too long
			desc := result.Description
			if len(desc) > 150 {
				desc = desc[:150] + "..."
			}
			response.WriteString(fmt.Sprintf("   📄 %s\n", desc))
		}

		response.WriteString(fmt.Sprintf("   🎯 Similarity: %.1f%% | 🔗 https://jira.hpe.com/browse/%s\n", result.SimilarityScore*100, result.IssueKey))
		response.WriteString(fmt.Sprintf("\n"))
	}

	return response.String()
}

// RegisterChatbotTools registers all chatbot tools with the MCP server
func RegisterChatbotTools(s *server.MCPServer, llm llms.Model, mongoClient *mongo.Client, kubeconfig string) {
	fmt.Printf("DEBUG: RegisterChatbotTools called\n")

	// Initialize Jira configuration
	jiraConfig := alerts.JiraSearchConfig{
		Endpoint:         "https://jira-search.hpe-srujan-ezaf.com/search",
		Timeout:          60,    // Increased timeout to 60 seconds
		VerifySSL:        false, // Disable SSL verification to handle certificate issues
		DefaultTopK:      5,     // Reduced to 5 results for more detailed output
		DefaultThreshold: 0.05,
	}

	fmt.Printf("DEBUG: Creating chatbot agent with Jira integration\n")
	chatbotAgent := NewKAgentChatbotAgentWithJira(llm, mongoClient, kubeconfig, jiraConfig)

	// Register chatbot query tool
	fmt.Printf("DEBUG: Registering chatbot_query tool\n")
	s.AddTool(mcp.NewTool("chatbot_query",
		mcp.WithDescription("Intelligent chatbot for Kubernetes alert queries with session memory"),
		mcp.WithString("query", mcp.Description("User query about alerts and issues"), mcp.Required()),
		mcp.WithString("timeRange", mcp.Description("Time range (e.g., '3h', '1d', '7d')")),
		mcp.WithString("limit", mcp.Description("Maximum number of alerts to analyze")),
		mcp.WithString("sessionId", mcp.Description("Session ID for maintaining conversation context")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("chatbot_query", chatbotAgent.handleChatbotQuery)))
	fmt.Printf("DEBUG: chatbot_query tool registered successfully\n")

	// Register pod status query tool
	fmt.Printf("DEBUG: Registering pod_status_query tool\n")
	s.AddTool(mcp.NewTool("pod_status_query",
		mcp.WithDescription("Query pod status and failures"),
		mcp.WithString("timeRange", mcp.Description("Time range (e.g., '2h', '1d')")),
		mcp.WithString("namespace", mcp.Description("Namespace to filter by")),
		mcp.WithString("status", mcp.Description("Pod status to filter by (e.g., 'Failed', 'Pending')")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("pod_status_query", chatbotAgent.handlePodStatusQuery)))
	fmt.Printf("DEBUG: pod_status_query tool registered successfully\n")

	// Register log display tool
	fmt.Printf("DEBUG: Registering log_display tool\n")
	s.AddTool(mcp.NewTool("log_display",
		mcp.WithDescription("Display collected logs from specific pods"),
		mcp.WithString("podName", mcp.Description("Pod name to get logs for"), mcp.Required()),
		mcp.WithString("namespace", mcp.Description("Namespace of the pod")),
		mcp.WithString("limit", mcp.Description("Maximum number of log entries to return")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("log_display", chatbotAgent.handleLogDisplay)))
	fmt.Printf("DEBUG: log_display tool registered successfully\n")

	// Register analysis display tool
	fmt.Printf("DEBUG: Registering analysis_display tool\n")
	s.AddTool(mcp.NewTool("analysis_display",
		mcp.WithDescription("Display analysis results for pods"),
		mcp.WithString("podName", mcp.Description("Pod name to get analysis for")),
		mcp.WithString("namespace", mcp.Description("Namespace to filter by")),
		mcp.WithString("timeRange", mcp.Description("Time range for analysis data")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("analysis_display", chatbotAgent.handleAnalysisDisplay)))
	fmt.Printf("DEBUG: analysis_display tool registered successfully\n")

	// Register enhanced remediation tool
	fmt.Printf("DEBUG: Registering enhanced_remediation tool\n")
	s.AddTool(mcp.NewTool("enhanced_remediation",
		mcp.WithDescription("Get enhanced remediation assistance for pod issues"),
		mcp.WithString("podName", mcp.Description("Pod name to get remediation for")),
		mcp.WithString("namespace", mcp.Description("Namespace of the pod")),
		mcp.WithString("issueType", mcp.Description("Type of issue to remediate")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("enhanced_remediation", chatbotAgent.handleEnhancedRemediation)))
	fmt.Printf("DEBUG: enhanced_remediation tool registered successfully\n")

	// Register remediation request tool
	fmt.Printf("DEBUG: Registering get_remediation tool\n")
	s.AddTool(mcp.NewTool("get_remediation",
		mcp.WithDescription("Get remediation script for specific alert with session context"),
		mcp.WithString("alertId", mcp.Description("Alert ID to get remediation for")),
		mcp.WithString("service", mcp.Description("Service name")),
		mcp.WithString("namespace", mcp.Description("Namespace")),
		mcp.WithString("sessionId", mcp.Description("Session ID for maintaining conversation context")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("get_remediation", chatbotAgent.handleGetRemediation)))
	fmt.Printf("DEBUG: get_remediation tool registered successfully\n")

	fmt.Printf("DEBUG: Enhanced chatbot tools registered successfully\n")
}
