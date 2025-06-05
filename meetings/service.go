// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package meetings

import (
	"github.com/mattermost/mattermost-plugin-ai/bots"
	"github.com/mattermost/mattermost-plugin-ai/conversations"
	"github.com/mattermost/mattermost-plugin-ai/i18n"
	"github.com/mattermost/mattermost-plugin-ai/llm"
	"github.com/mattermost/mattermost-plugin-ai/llmcontext"
	"github.com/mattermost/mattermost-plugin-ai/metrics"
	"github.com/mattermost/mattermost-plugin-ai/mmapi"
	"github.com/mattermost/mattermost-plugin-ai/streaming"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

const (
	CallsRecordingPostType = "custom_calls_recording"
	CallsBotUsername       = "calls"
	ZoomBotUsername        = "zoom"
)

// Service handles meeting summarization and transcription functionality
type Service struct {
	pluginAPI        *pluginapi.Client
	streamingService streaming.Service
	prompts          *llm.Prompts
	bots             *bots.MMBots
	i18n             *i18n.Bundle
	metricsService   metrics.Metrics
	db               *mmapi.DBClient
	contextBuilder   *llmcontext.Builder
	conversations    *conversations.Conversations

	ffmpegPath string
}

// NewService creates a new meetings service
func NewService(
	pluginAPI *pluginapi.Client,
	streamingService streaming.Service,
	prompts *llm.Prompts,
	bots *bots.MMBots,
	i18n *i18n.Bundle,
	metricsService metrics.Metrics,
	db *mmapi.DBClient,
	contextBuilder *llmcontext.Builder,
	conversations *conversations.Conversations,
) *Service {
	service := &Service{
		pluginAPI:        pluginAPI,
		streamingService: streamingService,
		prompts:          prompts,
		bots:             bots,
		i18n:             i18n,
		metricsService:   metricsService,
		db:               db,
		contextBuilder:   contextBuilder,
		conversations:    conversations,
	}

	service.ffmpegPath = resolveFFMPEGPath()
	if service.ffmpegPath == "" {
		service.pluginAPI.Log.Error("ffmpeg not installed, transcriptions will be disabled.")
	}

	return service
}
