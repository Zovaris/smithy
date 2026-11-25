# 0012 · Official provider SDKs for the API-key backend

- Status: accepted
- Date: 2026-11-25

## Context and Problem Statement

v0.22 adds `smithy agent --backend api`: smithy's own agent loop on the user's API keys, behind
the same events as the agent CLIs. It must talk to Anthropic, OpenAI, Google Gemini and
OpenAI-compatible endpoints (OpenRouter, DeepSeek, Ollama, local servers) with streaming, prompt
caching and accurate usage. Hand-written HTTP clients for four wire formats would drift from the
APIs; the binary behind the skills must stay standard library only (ADR 0009).

## Decision Outcome

The harness build (`-tags harness`) may also depend on the official Go SDKs, latest release:
- `github.com/anthropics/anthropic-sdk-go` (Messages API, prompt caching via `cache_control`);
- `github.com/openai/openai-go/v3`: the Responses API for OpenAI, Chat Completions on a base URL
  for OpenAI-compatible endpoints (most of them, DeepSeek included, have no Responses API);
- `google.golang.org/genai` (Gemini API).

Only `internal/apiloop/provider` imports them, and every file there carries `//go:build harness`.
The loop itself (`internal/apiloop`: messages, tool calls, permissions, retries on 429/5xx with
backoff, pricing) is standard library and untagged, so it is tested in both builds; the skills
build's `smithy agent --backend api` says it needs the harness build (exit 2). SDK retries are
off so one retry policy applies to every provider. Keys come from `smithy auth` (the auth file
only), are handed to the SDK client in process and never go on argv, in env vars or in logs.

## Consequences

- Good: four providers and every OpenAI-compatible endpoint with little code; wire-format changes
  arrive as SDK updates.
- Bad: the harness build grows by the SDKs and their modules (genai brings gRPC protobuf types);
  `go.mod` lists them for both builds, so a test fails if the skills build links any of them.
