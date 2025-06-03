// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package anthropic

import (
	"bytes"
	"testing"

	anthropicSDK "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"

	"github.com/mattermost/mattermost-plugin-ai/llm"
)

func TestConversationToMessages(t *testing.T) {
	tests := []struct {
		name         string
		conversation []llm.Post
		wantSystem   string
		wantMessages []anthropicSDK.MessageParam
	}{
		{
			name: "basic conversation with system message",
			conversation: []llm.Post{
				{Role: llm.PostRoleSystem, Message: "You are a helpful assistant"},
				{Role: llm.PostRoleUser, Message: "Hello"},
				{Role: llm.PostRoleBot, Message: "Hi there!"},
			},
			wantSystem: "You are a helpful assistant",
			wantMessages: []anthropicSDK.MessageParam{
				{
					Role: anthropicSDK.MessageParamRoleUser,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("Hello"),
					},
				},
				{
					Role: anthropicSDK.MessageParamRoleAssistant,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("Hi there!"),
					},
				},
			},
		},
		{
			name: "multiple messages from same role",
			conversation: []llm.Post{
				{Role: llm.PostRoleUser, Message: "First message"},
				{Role: llm.PostRoleUser, Message: "Second message"},
				{Role: llm.PostRoleBot, Message: "First response"},
				{Role: llm.PostRoleBot, Message: "Second response"},
			},
			wantSystem: "",
			wantMessages: []anthropicSDK.MessageParam{
				{
					Role: anthropicSDK.MessageParamRoleUser,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("First message"),
						anthropicSDK.NewTextBlock("Second message"),
					},
				},
				{
					Role: anthropicSDK.MessageParamRoleAssistant,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("First response"),
						anthropicSDK.NewTextBlock("Second response"),
					},
				},
			},
		},
		{
			name: "conversation with image",
			conversation: []llm.Post{
				{Role: llm.PostRoleUser, Message: "Look at this:",
					Files: []llm.File{
						{
							MimeType: "image/jpeg",
							Reader:   bytes.NewReader([]byte("fake-image-data")),
						},
					}},
				{Role: llm.PostRoleBot, Message: "I see the image"},
			},
			wantSystem: "",
			wantMessages: []anthropicSDK.MessageParam{
				{
					Role: anthropicSDK.MessageParamRoleUser,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("Look at this:"),
						anthropicSDK.NewImageBlockBase64("image/jpeg", "ZmFrZS1pbWFnZS1kYXRh"),
					},
				},
				{
					Role: anthropicSDK.MessageParamRoleAssistant,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("I see the image"),
					},
				},
			},
		},
		{
			name: "unsupported image type",
			conversation: []llm.Post{
				{Role: llm.PostRoleUser, Files: []llm.File{
					{
						MimeType: "image/tiff",
						Reader:   bytes.NewReader([]byte("fake-tiff-data")),
					},
				}},
			},
			wantSystem: "",
			wantMessages: []anthropicSDK.MessageParam{
				{
					Role: anthropicSDK.MessageParamRoleUser,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("[Unsupported image type: image/tiff]"),
					},
				},
			},
		},
		{
			name: "complex back and forth with repeated roles",
			conversation: []llm.Post{
				{Role: llm.PostRoleUser, Message: "First question"},
				{Role: llm.PostRoleBot, Message: "First answer"},
				{Role: llm.PostRoleUser, Message: "Follow up 1"},
				{Role: llm.PostRoleUser, Message: "Follow up 2"},
				{Role: llm.PostRoleUser, Message: "Follow up 3"},
				{Role: llm.PostRoleBot, Message: "Response 1"},
				{Role: llm.PostRoleBot, Message: "Response 2"},
				{Role: llm.PostRoleBot, Message: "Response 3"},
				{Role: llm.PostRoleUser, Message: "Final question"},
			},
			wantSystem: "",
			wantMessages: []anthropicSDK.MessageParam{
				{
					Role: anthropicSDK.MessageParamRoleUser,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("First question"),
					},
				},
				{
					Role: anthropicSDK.MessageParamRoleAssistant,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("First answer"),
					},
				},
				{
					Role: anthropicSDK.MessageParamRoleUser,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("Follow up 1"),
						anthropicSDK.NewTextBlock("Follow up 2"),
						anthropicSDK.NewTextBlock("Follow up 3"),
					},
				},
				{
					Role: anthropicSDK.MessageParamRoleAssistant,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("Response 1"),
						anthropicSDK.NewTextBlock("Response 2"),
						anthropicSDK.NewTextBlock("Response 3"),
					},
				},
				{
					Role: anthropicSDK.MessageParamRoleUser,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("Final question"),
					},
				},
			},
		},
		{
			name: "multiple roles with multiple images",
			conversation: []llm.Post{
				{Role: llm.PostRoleUser, Message: "Look at these images:",
					Files: []llm.File{
						{
							MimeType: "image/jpeg",
							Reader:   bytes.NewReader([]byte("image-1")),
						},
						{
							MimeType: "image/png",
							Reader:   bytes.NewReader([]byte("image-2")),
						},
					},
				},
				{Role: llm.PostRoleBot, Message: "I see them"},
				{Role: llm.PostRoleUser, Message: "Here are more:",
					Files: []llm.File{
						{
							MimeType: "image/webp",
							Reader:   bytes.NewReader([]byte("image-3")),
						},
						{
							MimeType: "image/tiff", // unsupported
							Reader:   bytes.NewReader([]byte("image-4")),
						},
						{
							MimeType: "image/gif",
							Reader:   bytes.NewReader([]byte("image-5")),
						},
					},
				},
			},
			wantSystem: "",
			wantMessages: []anthropicSDK.MessageParam{
				{
					Role: anthropicSDK.MessageParamRoleUser,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("Look at these images:"),
						anthropicSDK.NewImageBlockBase64("image/jpeg", "aW1hZ2UtMQ=="),
						anthropicSDK.NewImageBlockBase64("image/png", "aW1hZ2UtMg=="),
					},
				},
				{
					Role: anthropicSDK.MessageParamRoleAssistant,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("I see them"),
					},
				},
				{
					Role: anthropicSDK.MessageParamRoleUser,
					Content: []anthropicSDK.ContentBlockParamUnion{
						anthropicSDK.NewTextBlock("Here are more:"),
						anthropicSDK.NewImageBlockBase64("image/webp", "aW1hZ2UtMw=="),
						anthropicSDK.NewTextBlock("[Unsupported image type: image/tiff]"),
						anthropicSDK.NewImageBlockBase64("image/gif", "aW1hZ2UtNQ=="),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSystem, gotMessages := conversationToMessages(tt.conversation)
			assert.Equal(t, tt.wantSystem, gotSystem)
			assert.Equal(t, tt.wantMessages, gotMessages)
		})
	}
}
