package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// ClaudeAgent implements Agent using the Anthropic Go SDK.
type ClaudeAgent struct {
	client *anthropic.Client
	model  string
}

// NewClaude creates a ClaudeAgent. opts are passed to anthropic.NewClient,
// useful for injecting a test base URL.
func NewClaude(apiKey, model string, opts ...option.RequestOption) *ClaudeAgent {
	allOpts := append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
	c := anthropic.NewClient(allOpts...)
	return &ClaudeAgent{
		client: &c,
		model:  model,
	}
}

func (a *ClaudeAgent) Complete(ctx context.Context, prompt string) (string, error) {
	resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: 4096,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("claude complete: %w", err)
	}
	return extractText(resp.Content), nil
}

func (a *ClaudeAgent) CompleteWithTools(ctx context.Context, prompt string, tools []Tool) (string, []ToolCall, error) {
	apiTools := toAPITools(tools)
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
	}

	var allCalls []ToolCall

	for {
		resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     a.model,
			MaxTokens: 4096,
			Tools:     apiTools,
			Messages:  messages,
		})
		if err != nil {
			return "", nil, fmt.Errorf("claude tool call: %w", err)
		}

		// Append assistant turn to history.
		messages = append(messages, resp.ToParam())

		if resp.StopReason != anthropic.StopReasonToolUse {
			return extractText(resp.Content), allCalls, nil
		}

		var toolResults []anthropic.ContentBlockParamUnion
		for _, block := range resp.Content {
			tc, ok := block.AsAny().(anthropic.ToolUseBlock)
			if !ok {
				continue
			}
			allCalls = append(allCalls, ToolCall{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: json.RawMessage(tc.Input),
			})
			toolResults = append(toolResults,
				anthropic.NewToolResultBlock(tc.ID, "", false))
		}
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}
}

func extractText(blocks []anthropic.ContentBlockUnion) string {
	for _, b := range blocks {
		if tb, ok := b.AsAny().(anthropic.TextBlock); ok {
			return tb.Text
		}
	}
	return ""
}

func toAPITools(tools []Tool) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, len(tools))
	for i, t := range tools {
		var props any
		_ = json.Unmarshal(t.InputSchema, &props)
		out[i] = anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: props,
				},
			},
		}
	}
	return out
}
