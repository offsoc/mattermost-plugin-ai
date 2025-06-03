// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package api

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mattermost/mattermost-plugin-ai/agents"
	"github.com/mattermost/mattermost-plugin-ai/metrics"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

const (
	ContextPostKey    = "post"
	ContextChannelKey = "channel"
	ContextBotKey     = "bot"
)

// API represents the HTTP API functionality for the plugin
type API struct {
	agents         *agents.AgentsService
	pluginAPI      *pluginapi.Client
	metricsService metrics.Metrics
	metricsHandler http.Handler
}

// New creates a new API instance
func New(agentsService *agents.AgentsService, pluginAPI *pluginapi.Client, metricsService metrics.Metrics) *API {
	return &API{
		agents:         agentsService,
		pluginAPI:      pluginAPI,
		metricsService: metricsService,
		metricsHandler: metrics.NewMetricsHandler(metricsService),
	}
}

// ServeHTTP handles HTTP requests to the plugin
func (a *API) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	router := gin.Default()
	router.Use(a.ginlogger)
	router.Use(a.metricsMiddleware)

	interPluginRoute := router.Group("/inter-plugin/v1")
	interPluginRoute.Use(a.interPluginAuthorizationRequired)
	interPluginRoute.POST("/simple_completion", a.handleInterPluginSimpleCompletion)

	router.Use(a.MattermostAuthorizationRequired)

	router.GET("/ai_threads", a.handleGetAIThreads)
	router.GET("/ai_bots", a.handleGetAIBots)

	botRequiredRouter := router.Group("")
	botRequiredRouter.Use(a.aiBotRequired)

	postRouter := botRequiredRouter.Group("/post/:postid")
	postRouter.Use(a.postAuthorizationRequired)
	postRouter.POST("/react", a.handleReact)
	postRouter.POST("/analyze", a.handleThreadAnalysis)
	postRouter.POST("/transcribe/file/:fileid", a.handleTranscribeFile)
	postRouter.POST("/summarize_transcription", a.handleSummarizeTranscription)
	postRouter.POST("/stop", a.handleStop)
	postRouter.POST("/regenerate", a.handleRegenerate)
	postRouter.POST("/tool_call", a.handleToolCall)
	postRouter.POST("/postback_summary", a.handlePostbackSummary)

	channelRouter := botRequiredRouter.Group("/channel/:channelid")
	channelRouter.Use(a.channelAuthorizationRequired)
	channelRouter.POST("/interval", a.handleInterval)

	adminRouter := router.Group("/admin")
	adminRouter.Use(a.mattermostAdminAuthorizationRequired)
	adminRouter.POST("/reindex", a.handleReindexPosts)
	adminRouter.GET("/reindex/status", a.handleGetJobStatus)
	adminRouter.POST("/reindex/cancel", a.handleCancelJob)

	searchRouter := botRequiredRouter.Group("/search")
	// Only returns search results
	searchRouter.POST("", a.handleSearchQuery)
	// Initiates a search and responds to the user in a DM with the selected bot
	searchRouter.POST("/run", a.handleRunSearch)

	router.ServeHTTP(w, r)
}

// ServeMetrics serves the metrics endpoint
func (a *API) ServeMetrics(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	a.metricsHandler.ServeHTTP(w, r)
}

func (a *API) metricsMiddleware(c *gin.Context) {
	a.metricsService.IncrementHTTPRequests()
	now := time.Now()

	c.Next()

	elapsed := float64(time.Since(now)) / float64(time.Second)

	status := c.Writer.Status()

	if status < 200 || status > 299 {
		a.metricsService.IncrementHTTPErrors()
	}

	endpoint := c.HandlerName()
	a.metricsService.ObserveAPIEndpointDuration(endpoint, c.Request.Method, strconv.Itoa(status), elapsed)
}

func (a *API) aiBotRequired(c *gin.Context) {
	botUsername := c.Query("botUsername")
	bot := a.agents.GetBotByUsernameOrFirst(botUsername)
	if bot == nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to get bot: %s", botUsername))
		return
	}
	c.Set(ContextBotKey, bot)
}

func (a *API) ginlogger(c *gin.Context) {
	c.Next()

	for _, ginErr := range c.Errors {
		a.pluginAPI.Log.Error(ginErr.Error())
	}
}

func (a *API) MattermostAuthorizationRequired(c *gin.Context) {
	userID := c.GetHeader("Mattermost-User-Id")
	if userID == "" {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
}

func (a *API) interPluginAuthorizationRequired(c *gin.Context) {
	pluginID := c.GetHeader("Mattermost-Plugin-ID")
	if pluginID != "" {
		return
	}
	c.AbortWithStatus(http.StatusUnauthorized)
}

// enforceEmptyBody checks if the request body is empty returning an error if not
func (a *API) enforceEmptyBody(c *gin.Context) error {
	// Check the body is empty
	if _, err := c.Request.Body.Read(make([]byte, 1)); err != io.EOF {
		return fmt.Errorf("request body must be empty")
	}
	return nil
}

func (a *API) handleGetAIThreads(c *gin.Context) {
	userID := c.GetHeader("Mattermost-User-Id")

	threads, err := a.agents.GetAIThreads(userID)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to get posts for bot DM: %w", err))
		return
	}

	c.JSON(http.StatusOK, threads)
}

type AIBotsResponse struct {
	Bots          []agents.AIBotInfo `json:"bots"`
	SearchEnabled bool               `json:"searchEnabled"`
}

func (a *API) handleGetAIBots(c *gin.Context) {
	userID := c.GetHeader("Mattermost-User-Id")
	bots, err := a.agents.GetAIBots(userID)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Check if search is enabled
	searchEnabled := a.agents.IsSearchEnabled()

	c.JSON(http.StatusOK, AIBotsResponse{
		Bots:          bots,
		SearchEnabled: searchEnabled,
	})
}
