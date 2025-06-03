// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package embeddings

import (
	"context"
	"encoding/json"
)

// Provider types
const (
	ProviderTypeOpenAI           = "openai"
	ProviderTypeOpenAICompatible = "openai-compatible"
)

// Vector store types
const (
	VectorStoreTypePGVector = "pgvector"
)

// Search types
const (
	SearchTypeComposite = "composite"
)

// PostDocument represents a Mattermost post with its metadata
type PostDocument struct {
	PostID      string // ID of the Mattermost post
	CreateAt    int64  // Creation timestamp of the referenced post, not when this was indexed
	TeamID      string
	ChannelID   string
	UserID      string
	Content     string
	IsChunk     bool
	ChunkIndex  int
	TotalChunks int
}

// SearchResult represents a single search result with its similarity score
type SearchResult struct {
	Document PostDocument
	Score    float32
}

// SearchOptions contains parameters for search operations
type SearchOptions struct {
	Limit         int
	MinScore      float32
	TeamID        string
	ChannelID     string
	UserID        string // User ID for permission checks
	CreatedAfter  int64
	CreatedBefore int64
}

// ChunkingOptions defines options for chunking documents
type ChunkingOptions struct {
	ChunkSize        int     `json:"chunkSize"`        // Maximum size of each chunk in characters
	ChunkOverlap     int     `json:"chunkOverlap"`     // Number of characters to overlap between chunks
	MinChunkSize     float64 `json:"minChunkSize"`     // Minimum chunk size as a fraction of max size (0.0-1.0)
	ChunkingStrategy string  `json:"chunkingStrategy"` // Strategy: sentences, paragraphs, or fixed
}

// DefaultChunkingOptions returns the default chunking options
func DefaultChunkingOptions() ChunkingOptions {
	return ChunkingOptions{
		ChunkSize:        1000,
		ChunkOverlap:     200,
		MinChunkSize:     0.75,
		ChunkingStrategy: "sentences",
	}
}

// EmbeddingSearch defines the high-level interface for storing and searching using embeddings
type EmbeddingSearch interface {
	// Store stores documents and handles embedding generation internally
	Store(ctx context.Context, docs []PostDocument) error

	// Search performs a similarity search using the query text
	Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)

	// Delete removes documents
	Delete(ctx context.Context, postIDs []string) error

	// Clear removes all documents
	Clear(ctx context.Context) error
}

// VectorStore defines the interface for vector storage and search operations
type VectorStore interface {
	// Store stores documents and their embeddings
	Store(ctx context.Context, docs []PostDocument, embeddings [][]float32) error

	// Search performs a similarity search using the provided embedding
	Search(ctx context.Context, embedding []float32, opts SearchOptions) ([]SearchResult, error)

	// Delete removes documents from the vector store
	Delete(ctx context.Context, postIDs []string) error

	// Clear removes all documents from the vector store
	Clear(ctx context.Context) error
}

// EmbeddingProvider defines the interface for embedding generation
type EmbeddingProvider interface {
	// CreateEmbedding generates embedding for the given text
	CreateEmbedding(ctx context.Context, text string) ([]float32, error)

	// BatchCreateEmbeddings generates embeddings for multiple texts
	BatchCreateEmbeddings(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the dimensionality of the embeddings
	Dimensions() int
}

// UpstreamConfig holds configuration for the upstream service
type UpstreamConfig struct {
	Type       string          `json:"type"`
	Parameters json.RawMessage `json:"parameters"`
}

// ServiceConfig holds configuration for the embedding search service
type EmbeddingSearchConfig struct {
	Type              string          `json:"type"`
	VectorStore       UpstreamConfig  `json:"vectorStore"`
	EmbeddingProvider UpstreamConfig  `json:"embeddingProvider"`
	Parameters        json.RawMessage `json:"parameters"`
	Dimensions        int             `json:"dimensions"`
	ChunkingOptions   ChunkingOptions `json:"chunkingOptions"`
}
