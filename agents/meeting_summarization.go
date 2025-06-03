// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package agents

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	sq "github.com/Masterminds/squirrel"

	"errors"

	"github.com/mattermost/mattermost-plugin-ai/i18n"
	"github.com/mattermost/mattermost-plugin-ai/llm"
	"github.com/mattermost/mattermost-plugin-ai/llm/subtitles"
	"github.com/mattermost/mattermost-plugin-ai/mmapi"
	"github.com/mattermost/mattermost/server/public/model"
)

const ContextTokenMargin = 1000
const WhisperAPILimit = 25 * 1000 * 1000 // 25 MB

func getCaptionsFileIDFromProps(post *model.Post) (fileID string, err error) {
	if post == nil {
		return "", errors.New("post is nil")
	}

	defer func() {
		if r := recover(); r != nil {
			err = errors.New("unable to parse captions on post")
		}
	}()

	captions, ok := post.GetProp("captions").([]interface{})
	if !ok || len(captions) == 0 {
		return "", errors.New("no captions on post")
	}

	// Calls will only ever have one for now.
	return captions[0].(map[string]interface{})["file_id"].(string), nil
}

func (p *AgentsService) createTranscription(recordingFileID string) (*subtitles.Subtitles, error) {
	if p.ffmpegPath == "" {
		return nil, errors.New("ffmpeg not installed")
	}

	recordingFileInfo, err := p.pluginAPI.File.GetInfo(recordingFileID)
	if err != nil {
		return nil, fmt.Errorf("unable to get calls file info: %w", err)
	}

	fileReader, err := p.pluginAPI.File.Get(recordingFileID)
	if err != nil {
		return nil, fmt.Errorf("unable to read calls file: %w", err)
	}

	var cmd *exec.Cmd
	if recordingFileInfo.Size > WhisperAPILimit {
		cmd = exec.Command(p.ffmpegPath, "-i", "pipe:0", "-ac", "1", "-map", "0:a:0", "-b:a", "32k", "-ar", "16000", "-f", "mp3", "pipe:1") //nolint:gosec
	} else {
		cmd = exec.Command(p.ffmpegPath, "-i", "pipe:0", "-f", "mp3", "pipe:1") //nolint:gosec
	}

	cmd.Stdin = fileReader

	audioReader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("couldn't create stdout pipe: %w", err)
	}

	errorReader, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("couldn't create stderr pipe: %w", err)
	}

	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("couldn't run ffmpeg: %w", err)
	}

	transcriber := p.getTranscribe()
	// Limit reader should probably error out instead of just silently failing
	transcription, err := transcriber.Transcribe(io.LimitReader(audioReader, WhisperAPILimit))
	if err != nil {
		return nil, fmt.Errorf("unable to transcribe: %w", err)
	}

	errout, err := io.ReadAll(errorReader)
	if err != nil {
		return nil, fmt.Errorf("unable to read stderr from ffmpeg: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		p.pluginAPI.Log.Debug("ffmpeg stderr: " + string(errout))
		return nil, fmt.Errorf("error while waiting for ffmpeg: %w", err)
	}

	return transcription, nil
}

func (p *AgentsService) newCallRecordingThread(bot *Bot, requestingUser *model.User, recordingPost *model.Post, channel *model.Channel, fileID string) (*model.Post, error) {
	siteURL := p.pluginAPI.Configuration.GetConfig().ServiceSettings.SiteURL
	T := i18n.LocalizerFunc(p.i18n, requestingUser.Locale)
	surePost := &model.Post{
		Message: T("copilot.summarize_recording", "Sure, I will summarize this recording: %s/_redirect/pl/%s\n", *siteURL, recordingPost.Id),
	}
	surePost.AddProp(NoRegen, "true")
	if err := p.botDMNonResponse(bot.mmBot.UserId, requestingUser.Id, surePost); err != nil {
		return nil, err
	}

	if err := p.summarizeCallRecording(bot, surePost.Id, requestingUser, fileID, channel); err != nil {
		return nil, err
	}

	return surePost, nil
}

func (p *AgentsService) newCallTranscriptionSummaryThread(bot *Bot, requestingUser *model.User, transcriptionPost *model.Post, channel *model.Channel) (*model.Post, error) {
	if len(transcriptionPost.FileIds) != 1 {
		return nil, errors.New("unexpected number of files in calls post")
	}

	siteURL := p.pluginAPI.Configuration.GetConfig().ServiceSettings.SiteURL
	T := i18n.LocalizerFunc(p.i18n, requestingUser.Locale)
	surePost := &model.Post{
		Message: T("copilot.summarize_transcription", "Sure, I will summarize this transcription: %s/_redirect/pl/%s\n", *siteURL, transcriptionPost.Id),
	}
	surePost.AddProp(NoRegen, "true")
	surePost.AddProp(ReferencedTranscriptPostID, transcriptionPost.Id)
	if err := p.botDMNonResponse(bot.mmBot.UserId, requestingUser.Id, surePost); err != nil {
		return nil, err
	}

	go func() (reterr error) {
		// Update to an error if we return one.
		defer func() {
			if reterr != nil {
				surePost.Message = T("copilot.summairize_subscription_error", "Sorry! Something went wrong. Check the server logs for details.")
				if err := p.pluginAPI.Post.UpdatePost(surePost); err != nil {
					p.pluginAPI.Log.Error("Failed to update post in error handling newCallTranscriptionSummaryThread", "error", err)
				}
				p.pluginAPI.Log.Error("Error in call recording post", "error", reterr)
			}
		}()

		transcriptionFileID, err := getCaptionsFileIDFromProps(transcriptionPost)
		if err != nil {
			return fmt.Errorf("unable to get transcription file id: %w", err)
		}
		transcriptionFileInfo, err := p.pluginAPI.File.GetInfo(transcriptionFileID)
		if err != nil {
			return fmt.Errorf("unable to get transcription file info: %w", err)
		}
		transcriptionFilePost, err := p.pluginAPI.Post.GetPost(transcriptionFileInfo.PostId)
		if err != nil {
			return fmt.Errorf("unable to get transcription file post: %w", err)
		}
		if transcriptionFilePost.ChannelId != channel.Id {
			return errors.New("strange configuration of calls transcription file")
		}
		transcriptionFileReader, err := p.pluginAPI.File.Get(transcriptionFileID)
		if err != nil {
			return fmt.Errorf("unable to read calls file: %w", err)
		}

		var transcription *subtitles.Subtitles
		if transcriptionFilePost.Type == "custom_zoom_chat" {
			transcription, err = subtitles.NewSubtitlesFromZoomChat(transcriptionFileReader)
			if err != nil {
				return fmt.Errorf("unable to parse transcription file: %w", err)
			}
		} else {
			transcription, err = subtitles.NewSubtitlesFromVTT(transcriptionFileReader)
			if err != nil {
				return fmt.Errorf("unable to parse transcription file: %w", err)
			}
		}

		requestContext := p.contextBuilder.BuildLLMContextUserRequest(
			bot,
			requestingUser,
			channel,
			p.contextBuilder.WithLLMContextDefaultTools(bot, mmapi.IsDMWith(bot.mmBot.UserId, channel)),
		)
		summaryStream, err := p.summarizeTranscription(bot, transcription, requestContext)
		if err != nil {
			return fmt.Errorf("unable to summarize transcription: %w", err)
		}

		summaryPost := &model.Post{
			RootId:    surePost.Id,
			ChannelId: surePost.ChannelId,
			Message:   "",
		}
		summaryPost.AddProp(ReferencedTranscriptPostID, transcriptionPost.Id)
		if err := p.streamResultToNewPost(bot.mmBot.UserId, requestingUser.Id, summaryStream, summaryPost, transcriptionPost.Id); err != nil {
			return fmt.Errorf("unable to stream result to post: %w", err)
		}

		return nil
	}() //nolint:errcheck

	return surePost, nil
}

func (p *AgentsService) summarizeCallRecording(bot *Bot, rootID string, requestingUser *model.User, recordingFileID string, channel *model.Channel) error {
	T := i18n.LocalizerFunc(p.i18n, requestingUser.Locale)

	transcriptPost := &model.Post{
		RootId:  rootID,
		Message: T("copilot.summarize_call_recording_processing", "Processing audio into transcription. This will take some time..."),
	}
	transcriptPost.AddProp(ReferencedRecordingFileID, recordingFileID)
	if err := p.botDMNonResponse(bot.mmBot.UserId, requestingUser.Id, transcriptPost); err != nil {
		return err
	}

	go func() (reterr error) {
		// Update to an error if we return one.
		defer func() {
			if reterr != nil {
				transcriptPost.Message = T("copilot.summarize_call_recording_processing_error", "Sorry! Something went wrong. Check the server logs for details.")
				if err := p.pluginAPI.Post.UpdatePost(transcriptPost); err != nil {
					p.pluginAPI.Log.Error("Failed to update post in error handling handleCallRecordingPost", "error", err)
				}
				p.pluginAPI.Log.Error("Error in call recording post", "error", reterr)
			}
		}()

		transcription, err := p.createTranscription(recordingFileID)
		if err != nil {
			return fmt.Errorf("failed to create transcription: %w", err)
		}

		transcriptFileInfo, err := p.pluginAPI.File.Upload(strings.NewReader(transcription.FormatVTT()), "transcript.txt", channel.Id)
		if err != nil {
			return fmt.Errorf("unable to upload transcript: %w", err)
		}

		llmContext := p.contextBuilder.BuildLLMContextUserRequest(
			bot,
			requestingUser,
			channel,
			p.contextBuilder.WithLLMContextDefaultTools(bot, channel.Type == model.ChannelTypeDirect),
		)
		summaryStream, err := p.summarizeTranscription(bot, transcription, llmContext)
		if err != nil {
			return fmt.Errorf("unable to summarize transcription: %w", err)
		}

		if err = p.updatePostWithFile(transcriptPost, transcriptFileInfo); err != nil {
			return fmt.Errorf("unable to update transcript post: %w", err)
		}

		ctx, err := p.getPostStreamingContext(context.Background(), transcriptPost.Id)
		if err != nil {
			return fmt.Errorf("unable to get post streaming context: %w", err)
		}
		defer p.finishPostStreaming(transcriptPost.Id)

		p.streamResultToPost(ctx, summaryStream, transcriptPost, requestingUser.Locale)

		return nil
	}() //nolint:errcheck

	return nil
}

func (p *AgentsService) summarizeTranscription(bot *Bot, transcription *subtitles.Subtitles, context *llm.Context) (*llm.TextStreamResult, error) {
	llmFormattedTranscription := transcription.FormatForLLM()
	tokens := p.GetLLM(bot.cfg).CountTokens(llmFormattedTranscription)
	tokenLimitWithMargin := int(float64(p.GetLLM(bot.cfg).InputTokenLimit())*0.75) - ContextTokenMargin
	if tokenLimitWithMargin < 0 {
		tokenLimitWithMargin = ContextTokenMargin / 2
	}
	isChunked := false
	if tokens > tokenLimitWithMargin {
		p.pluginAPI.Log.Debug("Transcription too long, summarizing in chunks.", "tokens", tokens, "limit", tokenLimitWithMargin)
		chunks := splitPlaintextOnSentences(llmFormattedTranscription, tokenLimitWithMargin*4)
		summarizedChunks := make([]string, 0, len(chunks))
		p.pluginAPI.Log.Debug("Split into chunks", "chunks", len(chunks))
		for _, chunk := range chunks {
			systemPrompt, err := p.prompts.Format(llm.PromptSummarizeChunkSystem, context)
			if err != nil {
				return nil, fmt.Errorf("unable to get summarize chunk prompt: %w", err)
			}
			request := llm.CompletionRequest{
				Posts: []llm.Post{
					{
						Role:    llm.PostRoleSystem,
						Message: systemPrompt,
					},
					{
						Role:    llm.PostRoleUser,
						Message: chunk,
					},
				},
				Context: context,
			}

			summarizedChunk, err := p.GetLLM(bot.cfg).ChatCompletionNoStream(request)
			if err != nil {
				return nil, fmt.Errorf("unable to get summarized chunk: %w", err)
			}

			summarizedChunks = append(summarizedChunks, summarizedChunk)
		}

		llmFormattedTranscription = strings.Join(summarizedChunks, "\n\n")
		isChunked = true
		p.pluginAPI.Log.Debug("Completed chunk summarization", "chunks", len(summarizedChunks), "tokens", p.GetLLM(bot.cfg).CountTokens(llmFormattedTranscription))
	}

	context.Parameters = map[string]any{"IsChunked": fmt.Sprintf("%t", isChunked)}
	systemPrompt, err := p.prompts.Format(llm.PromptMeetingSummarySystem, context)
	if err != nil {
		return nil, fmt.Errorf("unable to get meeting summary prompt: %w", err)
	}

	completionRequest := llm.CompletionRequest{
		Posts: []llm.Post{
			{
				Role:    llm.PostRoleSystem,
				Message: systemPrompt,
			},
			{
				Role:    llm.PostRoleUser,
				Message: llmFormattedTranscription,
			},
		},
		Context: context,
	}

	summaryStream, err := p.GetLLM(bot.cfg).ChatCompletion(completionRequest)
	if err != nil {
		return nil, fmt.Errorf("unable to get meeting summary: %w", err)
	}

	return summaryStream, nil
}

func (p *AgentsService) updatePostWithFile(post *model.Post, fileinfo *model.FileInfo) error {
	if _, err := p.execBuilder(p.builder.
		Update("FileInfo").
		Set("PostId", post.Id).
		Set("ChannelId", post.ChannelId).
		Where(sq.And{
			sq.Eq{"Id": fileinfo.Id},
			sq.Eq{"PostId": ""},
		})); err != nil {
		return fmt.Errorf("unable to update file info: %w", err)
	}

	post.FileIds = []string{fileinfo.Id}
	post.Message = ""
	if err := p.pluginAPI.Post.UpdatePost(post); err != nil {
		return fmt.Errorf("unable to update post: %w", err)
	}

	return nil
}
