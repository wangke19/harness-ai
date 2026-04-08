package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/wangke19/harness-ai/internal/agent"
)

func TestClaudeCompleteWithTools_ToolLoop(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++
		if callCount == 1 {
			http.ServeFile(w, r, "testdata/tool_use_response.json")
		} else {
			http.ServeFile(w, r, "testdata/final_response.json")
		}
	}))
	defer srv.Close()

	a := agent.NewClaude("fake-key", "claude-sonnet-4-6",
		option.WithBaseURL(srv.URL),
	)

	tools := []agent.Tool{{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	}}

	text, calls, err := a.CompleteWithTools(context.Background(), "Make a plan", tools)
	if err != nil {
		t.Fatalf("CompleteWithTools: %v", err)
	}
	if text != "Here is the plan." {
		t.Errorf("text: got %q", text)
	}
	if len(calls) != 1 || calls[0].Name != "read_file" {
		t.Errorf("calls: got %+v", calls)
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls (tool loop), got %d", callCount)
	}
}
