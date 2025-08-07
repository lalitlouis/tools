package alerts

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/kagent-dev/tools/internal/telemetry"
)

// JiraSearchConfig holds configuration for Jira search service
type JiraSearchConfig struct {
	Endpoint         string  `json:"endpoint"`
	Timeout          int     `json:"timeout"`
	VerifySSL        bool    `json:"verifySSL"`
	DefaultTopK      int     `json:"defaultTopK"`
	DefaultThreshold float64 `json:"defaultThreshold"`
}

// JiraSearchResult represents a single Jira issue from search results
type JiraSearchResult struct {
	IssueKey        string   `json:"issue_key"`
	IssueType       string   `json:"issue_type"`
	Summary         string   `json:"summary"`
	Description     string   `json:"description"`
	Status          string   `json:"status"`
	Priority        string   `json:"priority"`
	Assignee        string   `json:"assignee"`
	Components      []string `json:"components"`
	Labels          []string `json:"labels"`
	Resolution      string   `json:"resolution"`
	Created         string   `json:"created"` // Changed to string to handle custom time format
	Updated         string   `json:"updated"` // Changed to string to handle custom time format
	Rank            int      `json:"rank"`
	SimilarityScore float64  `json:"similarity_score"`
}

// JiraSearchResponse represents the complete response from Jira search service
type JiraSearchResponse struct {
	Success             bool               `json:"success"`
	Query               string             `json:"query"`
	Results             []JiraSearchResult `json:"results"`
	TotalFound          int                `json:"total_found"`
	ProcessingTime      string             `json:"processing_time"`
	SearchTimestamp     string             `json:"search_timestamp"`
	SimilarityThreshold float64            `json:"similarity_threshold"`
	ModelUsed           string             `json:"model_used"`
	NimEndpoint         string             `json:"nim_endpoint"`
	FaissDimensions     int                `json:"faiss_dimensions"`
	FaissVectors        int                `json:"faiss_vectors"`
	Error               string             `json:"error,omitempty"`
}

// JiraIntegrationTool provides tools for integrating with Jira search service
type JiraIntegrationTool struct {
	config JiraSearchConfig
	client *http.Client
}

// NewJiraIntegrationTool creates a new Jira integration tool
func NewJiraIntegrationTool(config JiraSearchConfig) *JiraIntegrationTool {
	// Set default endpoint if not provided
	if config.Endpoint == "" {
		config.Endpoint = "https://jira-search.hpe-srujan-ezaf.com/search"
	}

	// Set default timeout if not provided
	if config.Timeout == 0 {
		config.Timeout = 30
	}

	// Set default values if not provided
	if config.DefaultTopK == 0 {
		config.DefaultTopK = 10
	}
	if config.DefaultThreshold == 0 {
		config.DefaultThreshold = 0.05
	}

	// Create HTTP transport with proper settings
	transport := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
		DisableKeepAlives:  false,
		ForceAttemptHTTP2:  true,
	}

	// Configure proxy settings if environment variables are set
	if proxyURL := os.Getenv("HTTP_PROXY"); proxyURL != "" {
		if proxy, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxy)
			fmt.Printf("DEBUG: Using HTTP proxy: %s\n", proxyURL)
		}
	}

	// Configure SSL certificate verification
	if !config.VerifySSL {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
		fmt.Printf("DEBUG: SSL certificate verification disabled\n")
	}

	// Create HTTP client with transport
	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(config.Timeout) * time.Second,
	}

	fmt.Printf("DEBUG: Creating JiraIntegrationTool with endpoint: %s\n", config.Endpoint)

	return &JiraIntegrationTool{
		config: config,
		client: client,
	}
}

// SearchJiraIssues searches for similar Jira issues based on a query
func (j *JiraIntegrationTool) SearchJiraIssues(ctx context.Context, query string, topK int, scoreThreshold float64) (*JiraSearchResponse, error) {
	// Prepare request payload
	payload := map[string]interface{}{
		"query":           query,
		"top_k":           topK,
		"score_threshold": scoreThreshold,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %v", err)
	}

	// Debug: Print the endpoint and payload
	fmt.Printf("DEBUG: Making Jira search request to: %s\n", j.config.Endpoint)
	fmt.Printf("DEBUG: Payload: %s\n", string(payloadBytes))
	fmt.Printf("DEBUG: Client timeout: %d seconds\n", j.config.Timeout)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", j.config.Endpoint, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Make the request with timeout tracking
	fmt.Printf("DEBUG: Starting Jira API call...\n")
	startTime := time.Now()
	resp, err := j.client.Do(req)
	elapsed := time.Since(startTime)
	fmt.Printf("DEBUG: Jira API call completed in %v\n", elapsed)

	if err != nil {
		fmt.Printf("DEBUG: HTTP request failed: %v\n", err)
		return nil, fmt.Errorf("failed to make HTTP request: %v", err)
	}

	fmt.Printf("DEBUG: Got response, status: %d\n", resp.StatusCode)
	defer resp.Body.Close()

	// Debug: Print response status
	fmt.Printf("DEBUG: Jira search response status: %d\n", resp.StatusCode)

	// Read response body for debugging
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("DEBUG: Failed to read response body: %v\n", err)
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}
	fmt.Printf("DEBUG: Jira search response body: %s\n", string(bodyBytes))

	// Parse response
	var searchResponse JiraSearchResponse
	if err := json.Unmarshal(bodyBytes, &searchResponse); err != nil {
		fmt.Printf("DEBUG: Failed to decode JSON response: %v\n", err)
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	if !searchResponse.Success {
		fmt.Printf("DEBUG: Jira search failed with error: %s\n", searchResponse.Error)
		return nil, fmt.Errorf("Jira search failed: %s", searchResponse.Error)
	}

	return &searchResponse, nil
}

// handleSearchJiraIssues handles the MCP tool request for searching Jira issues
func (j *JiraIntegrationTool) handleSearchJiraIssues(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := mcp.ParseString(request, "query", "")
	topKStr := mcp.ParseString(request, "topK", fmt.Sprintf("%d", j.config.DefaultTopK))
	scoreThresholdStr := mcp.ParseString(request, "scoreThreshold", fmt.Sprintf("%f", j.config.DefaultThreshold))

	// Parse topK as int
	topK := j.config.DefaultTopK
	if topKStr != "" {
		if parsed, err := strconv.Atoi(topKStr); err == nil {
			topK = parsed
		}
	}

	// Parse score threshold as float
	scoreThreshold := j.config.DefaultThreshold
	if scoreThresholdStr != "" {
		if parsed, err := strconv.ParseFloat(scoreThresholdStr, 64); err == nil {
			scoreThreshold = parsed
		}
	}

	if query == "" {
		return mcp.NewToolResultError("Query parameter is required"), nil
	}

	// Validate parameters
	if topK < 1 || topK > 100 {
		return mcp.NewToolResultError("topK must be between 1 and 100"), nil
	}

	if scoreThreshold < 0 || scoreThreshold > 1 {
		return mcp.NewToolResultError("scoreThreshold must be between 0 and 1"), nil
	}

	// Search for Jira issues
	searchResponse, err := j.SearchJiraIssues(ctx, query, topK, scoreThreshold)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to search Jira issues: %v", err)), nil
	}

	// Convert response to JSON
	resultJSON, err := json.MarshalIndent(searchResponse, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resultJSON)), nil
}

// handleCollectAlertDataWithJira enhances the existing alert collection with Jira integration
func (a *AlertCollectorTool) handleCollectAlertDataWithJira(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// For now, we'll create a simplified version that doesn't depend on the complex integration
	// This can be enhanced later when we have the proper Jira integration setup

	// Extract information for Jira search
	targetService := mcp.ParseString(request, "targetService", "")
	namespace := mcp.ParseString(request, "namespace", "")

	// Build a basic Jira search query
	jiraQuery := fmt.Sprintf("%s %s pod kubernetes", targetService, namespace)

	// Create a mock Jira response for demonstration
	mockJiraResponse := JiraSearchResponse{
		Success: true,
		Query:   jiraQuery,
		Results: []JiraSearchResult{
			{
				IssueKey:        "EZAF-1234",
				IssueType:       "Bug",
				Summary:         fmt.Sprintf("Sample issue for %s in %s", targetService, namespace),
				Status:          "Open",
				Priority:        "Medium",
				Assignee:        "Sample User",
				SimilarityScore: 0.85,
			},
		},
		TotalFound: 1,
	}

	// Add Jira results to the response
	result := map[string]interface{}{
		"targetService": targetService,
		"namespace":     namespace,
		"jiraIssues":    mockJiraResponse.Results,
		"jiraQuery":     jiraQuery,
		"timestamp":     time.Now().Format(time.RFC3339),
	}

	// Convert enhanced data back to JSON
	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal enhanced result: %v", err)), nil
	}

	return mcp.NewToolResultText(string(resultJSON)), nil
}

// buildJiraSearchQuery builds a search query for Jira based on alert information
func buildJiraSearchQuery(alertData map[string]interface{}, targetService, namespace string) string {
	var queryParts []string

	// Add service name
	if targetService != "" {
		queryParts = append(queryParts, targetService)
	}

	// Add namespace if it's not default
	if namespace != "" && namespace != "default" {
		queryParts = append(queryParts, namespace)
	}

	// Extract pod information for better search
	if pods, ok := alertData["pods"].([]interface{}); ok {
		for _, pod := range pods {
			if podMap, ok := pod.(map[string]interface{}); ok {
				if status, ok := podMap["status"].(map[string]interface{}); ok {
					if phase, ok := status["phase"].(string); ok {
						if phase != "Running" {
							queryParts = append(queryParts, phase)
						}
					}
					if reason, ok := status["reason"].(string); ok && reason != "" {
						queryParts = append(queryParts, reason)
					}
				}
			}
		}
	}

	// Add common Kubernetes terms
	queryParts = append(queryParts, "pod", "kubernetes", "k8s")

	// Join all parts
	return strings.Join(queryParts, " ")
}

// updateAlertDataWithJira updates MongoDB documents with Jira search results
func (a *AlertCollectorTool) updateAlertDataWithJira(ctx context.Context, alertData map[string]interface{}, jiraResponse JiraSearchResponse) {
	targetService := alertData["targetService"].(string)
	namespace := alertData["namespace"].(string)

	// Update pod alerts with Jira information
	if a.mongoClient != nil {
		collection := a.mongoClient.Database("kagent-alerts").Collection("pod_alerts")

		// Find existing pod alerts for this service and namespace
		filter := bson.M{
			"service":   targetService,
			"namespace": namespace,
		}

		update := bson.M{
			"$set": bson.M{
				"jiraIssues":          jiraResponse.Results,
				"jiraSearchQuery":     alertData["jiraSearchQuery"],
				"jiraSearchTimestamp": alertData["jiraSearchTimestamp"],
				"updatedAt":           time.Now(),
			},
		}

		_, err := collection.UpdateMany(ctx, filter, update)
		if err != nil {
			fmt.Printf("DEBUG: Failed to update pod alerts with Jira data: %v\n", err)
		} else {
			fmt.Printf("DEBUG: Successfully updated pod alerts with Jira data\n")
		}
	}
}

// RegisterJiraTools registers Jira-related tools with the MCP server
func RegisterJiraTools(s *server.MCPServer, jiraConfig JiraSearchConfig) {
	jiraTool := NewJiraIntegrationTool(jiraConfig)

	// Register Jira search tool
	s.AddTool(mcp.NewTool("search_jira_issues",
		mcp.WithDescription("Search for similar Jira issues based on a query"),
		mcp.WithString("query", mcp.Description("Search query for Jira issues"), mcp.Required()),
		mcp.WithString("topK", mcp.Description("Maximum number of results to return (1-100)")),
		mcp.WithString("scoreThreshold", mcp.Description("Similarity score threshold (0.0-1.0)")),
	), telemetry.AdaptToolHandler(telemetry.WithTracing("search_jira_issues", jiraTool.handleSearchJiraIssues)))

	fmt.Printf("DEBUG: Jira search tool registered successfully\n")
}
