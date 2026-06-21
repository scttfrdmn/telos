// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package gateway

import (
	"context"

	llmadapter "github.com/scttfrdmn/agenkit-go/adapter/llm"
	"github.com/scttfrdmn/agenkit-go/agenkit"
)

// fakeLLM is a deterministic agenkit adapter/llm.LLM for tests. It returns a
// fixed reply and attaches usage metadata in the EXACT shape the real Bedrock
// and Ollama adapters use (Metadata["usage"] = {prompt_tokens, completion_tokens,
// total_tokens, ...}). Using it through the real *llmBackend* means tests
// exercise the actual usage-normalization path, not a shortcut.
type fakeLLM struct {
	model        string
	prompt, comp int  // token counts to report
	cacheRead    int  // cache-read tokens to report (0 = none)
	intUsage     bool // true: report ints (Ollama-style); false: int32 (Bedrock-style)
	reply        string
	err          error
}

func (f *fakeLLM) Complete(ctx context.Context, messages []*agenkit.Message, opts ...llmadapter.CallOption) (*agenkit.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reply := f.reply
	if reply == "" {
		reply = "ok"
	}
	msg := agenkit.NewMessage("agent", reply)
	usage := map[string]interface{}{}
	if f.intUsage {
		usage["prompt_tokens"] = f.prompt
		usage["completion_tokens"] = f.comp
		usage["total_tokens"] = f.prompt + f.comp
		if f.cacheRead > 0 {
			usage["cache_read_tokens"] = f.cacheRead
		}
	} else {
		usage["prompt_tokens"] = int32(f.prompt)
		usage["completion_tokens"] = int32(f.comp)
		usage["total_tokens"] = int32(f.prompt + f.comp)
		if f.cacheRead > 0 {
			usage["cache_read_tokens"] = int32(f.cacheRead)
		}
	}
	msg.WithMetadata("usage", usage)
	msg.WithMetadata("model", f.model)
	return msg, nil
}

func (f *fakeLLM) Stream(ctx context.Context, messages []*agenkit.Message, opts ...llmadapter.CallOption) (<-chan *agenkit.Message, error) {
	ch := make(chan *agenkit.Message)
	close(ch)
	return ch, nil
}

func (f *fakeLLM) Model() string       { return f.model }
func (f *fakeLLM) Unwrap() interface{} { return f }

// fakeBackend wraps a fakeLLM as a gateway backend with a chosen name and
// billing posture — so tests can stand up a "cloud-like" (bills) and a
// "local-like" (synthesized) backend that are otherwise identical.
func fakeBackend(name string, bills bool, llm *fakeLLM) Backend {
	return &llmBackend{id: name, billsReal: bills, llm: llm}
}
