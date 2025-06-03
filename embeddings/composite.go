// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package embeddings

import (
	"context"
)

// CompositeSearch implements EmbeddingSearch using separate vector store and embedding provider
type CompositeSearch struct {
	store    VectorStore
	provider EmbeddingProvider
	options  ChunkingOptions
}

// NewCompositeSearch creates a new CompositeSearch with required chunking options
func NewCompositeSearch(store VectorStore, provider EmbeddingProvider, options ChunkingOptions) *CompositeSearch {
	return &CompositeSearch{
		store:    store,
		provider: provider,
		options:  options,
	}
}

// SetChunkingOptions updates the chunking options
func (c *CompositeSearch) SetChunkingOptions(options ChunkingOptions) {
	c.options = options
}

// Store chunks documents, generates embeddings, and stores them
func (c *CompositeSearch) Store(ctx context.Context, docs []PostDocument) error {
	// Apply chunking to each document
	var chunkedDocs []PostDocument
	for _, doc := range docs {
		chunks := ChunkPostDocument(doc, c.options)
		chunkedDocs = append(chunkedDocs, chunks...)
	}

	// Extract texts for embedding
	texts := make([]string, len(chunkedDocs))
	for i, doc := range chunkedDocs {
		texts[i] = doc.Content
	}

	// Generate embeddings for all chunks
	embeddings, err := c.provider.BatchCreateEmbeddings(ctx, texts)
	if err != nil {
		return err
	}

	// Store the chunks and their embeddings
	return c.store.Store(ctx, chunkedDocs, embeddings)
}

// Search performs a semantic search and merges results from chunks of the same document
func (c *CompositeSearch) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	// Generate embedding for the query
	embedding, err := c.provider.CreateEmbedding(ctx, query)
	if err != nil {
		return nil, err
	}

	// Search for matching chunks
	results, err := c.store.Search(ctx, embedding, opts)
	if err != nil {
		return nil, err
	}

	return results, nil
}

// Delete removes documents and their chunks
func (c *CompositeSearch) Delete(ctx context.Context, postIDs []string) error {
	return c.store.Delete(ctx, postIDs)
}

// Clear removes all documents and chunks
func (c *CompositeSearch) Clear(ctx context.Context) error {
	return c.store.Clear(ctx)
}
