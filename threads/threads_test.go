// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package threads_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/mattermost/mattermost-plugin-ai/evals"
	"github.com/mattermost/mattermost-plugin-ai/llm"
	"github.com/mattermost/mattermost-plugin-ai/llm/mocks"
	"github.com/mattermost/mattermost-plugin-ai/mmapi"
	mmapimocks "github.com/mattermost/mattermost-plugin-ai/mmapi/mocks"
	"github.com/mattermost/mattermost-plugin-ai/prompts"
	"github.com/mattermost/mattermost-plugin-ai/threads"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestThreadsAnalyze(t *testing.T) {
	tests := []struct {
		name             string
		postID           string
		promptName       string
		threadData       *mmapi.ThreadData
		threadDataErr    error
		expectedLLMCalls int
		llmError         error
		expectedError    bool
		errorContains    string
	}{
		{
			name:             "success",
			postID:           "post123",
			promptName:       prompts.PromptSummarizeThreadSystem,
			threadData:       &mmapi.ThreadData{Posts: []*model.Post{{Id: "post123", Message: "Test message", UserId: "user123"}}},
			threadDataErr:    nil,
			expectedLLMCalls: 1,
			llmError:         nil,
			expectedError:    false,
		},
		{
			name:             "thread data error",
			postID:           "post123",
			promptName:       prompts.PromptSummarizeThreadSystem,
			threadData:       nil,
			threadDataErr:    errors.New("failed to get thread data"),
			expectedLLMCalls: 0,
			llmError:         nil,
			expectedError:    true,
			errorContains:    "failed to create initial posts",
		},
		{
			name:             "llm error",
			postID:           "post123",
			promptName:       prompts.PromptSummarizeThreadSystem,
			threadData:       &mmapi.ThreadData{Posts: []*model.Post{{Id: "post123", Message: "Test message", UserId: "user123"}}},
			threadDataErr:    nil,
			expectedLLMCalls: 1,
			llmError:         errors.New("llm error"),
			expectedError:    true,
			errorContains:    "llm error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			mockLLM := mocks.NewMockLanguageModel(t)
			mockClient := mmapimocks.NewMockClient(t)
			prompts, err := llm.NewPrompts(prompts.PromptsFolder)
			require.NoError(t, err)

			// Create context with requesting user
			ctx := llm.NewContext()
			requestingUser := &model.User{
				Id:       "requester123",
				Username: "testuser",
				Locale:   "en",
			}
			ctx.RequestingUser = requestingUser

			// Set up mock for GetPostThread
			if tc.threadDataErr == nil {
				postList := &model.PostList{
					Order: []string{tc.postID},
					Posts: map[string]*model.Post{
						tc.postID: tc.threadData.Posts[0],
					},
				}
				mockClient.EXPECT().GetPostThread(tc.postID).Return(postList, nil)
				mockClient.EXPECT().GetUser(tc.threadData.Posts[0].UserId).Return(&model.User{
					Id:       tc.threadData.Posts[0].UserId,
					Username: "testuser123",
				}, nil)
			} else {
				mockClient.EXPECT().GetPostThread(tc.postID).Return(nil, tc.threadDataErr)
			}

			if tc.expectedLLMCalls > 0 {
				mockLLM.EXPECT().ChatCompletion(mock.Anything).Return(&llm.TextStreamResult{}, tc.llmError)
			}

			threadService := threads.New(mockLLM, prompts, mockClient)

			// Execute
			result, err := threadService.Analyze(tc.postID, ctx, tc.promptName)

			// Assert
			if tc.expectedError {
				assert.Error(t, err)
				if tc.errorContains != "" {
					assert.Contains(t, err.Error(), tc.errorContains)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestThreadsSummarizeFromExportedData(t *testing.T) {
	// Define the evaluation rubrics for each thread
	evalConfigs := []struct {
		filename string
		rubrics  []string
	}{
		{
			filename: "eval_timed_dnd.json",
			rubrics: []string{
				"mentions that the issue being discussed is a consistancy isue on time units of seconds vs milliseconds",
				"contains the usernames involved as @mentions if referenced",
			},
		},
	}

	for _, config := range evalConfigs {
		testName := "thread from " + config.filename

		evals.Run(t, testName, func(t *evals.EvalT) {
			// Load thread data from the JSON file
			path := filepath.Join(".", config.filename)
			threadData := evals.LoadThreadFromJSON(t, path)

			// Create the mock client with the thread data
			mockClient := mockThread(t, threadData)

			// Create context with requesting user and add channel and team info
			llmContext := llm.NewContext()
			llmContext.RequestingUser = &model.User{
				Id:       model.NewId(),
				Username: "bill",
				Locale:   "en",
			}
			llmContext.Channel = threadData.Channel
			llmContext.Team = threadData.Team

			// Do the thread summarization
			threadService := threads.New(t.LLM, t.Prompts, mockClient)
			result, err := threadService.Summarize(threadData.RootPost.Id, llmContext)
			require.NoError(t, err)
			require.NotNil(t, result)
			summary, err := result.ReadAll()
			require.NoError(t, err)
			assert.NotEmpty(t, summary, "Expected a non-empty thread summary")

			// Evaluate the summary against the rubric
			for _, rubric := range config.rubrics {
				evals.LLMRubricT(t, rubric, summary)
			}
		})
	}
}

func mockThread(t *evals.EvalT, threadData *evals.ThreadExport) *mmapimocks.MockClient {
	// Mock pluginapi returning thread
	mockClient := mmapimocks.NewMockClient(t.T)
	mockClient.EXPECT().GetPostThread(threadData.RootPost.Id).Return(threadData.PostList, nil)

	// Mock users
	for userID, user := range threadData.Users {
		mockClient.EXPECT().GetUser(userID).Return(user, nil)
	}

	return mockClient
}
