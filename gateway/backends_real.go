// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import (
	"context"
	"fmt"

	llmadapter "github.com/scttfrdmn/agenkit-go/adapter/llm"
)

// NewBedrockBackend builds the cloud backend over agenkit's Bedrock adapter.
// Bedrock bills for real, so its costs are NOT synthesized.
func NewBedrockBackend(ctx context.Context, modelID, region string) (Backend, error) {
	llm, err := llmadapter.NewBedrockLLM(ctx, llmadapter.BedrockConfig{
		ModelID: modelID,
		Region:  region,
	})
	if err != nil {
		return nil, fmt.Errorf("gateway: bedrock backend: %w", err)
	}
	return &llmBackend{id: "bedrock", billsReal: true, llm: llm}, nil
}

// NewOllamaBackend builds the local backend over agenkit's Ollama adapter. A
// local model bills nothing, so bills()==false — the gateway SYNTHESIZES its
// cost. baseURL defaults to http://localhost:11434 when empty.
func NewOllamaBackend(model, baseURL string) Backend {
	llm := llmadapter.NewOllamaLLM(model, baseURL)
	return &llmBackend{id: "ollama", billsReal: false, llm: llm}
}
