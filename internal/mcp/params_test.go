package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// P-20: a parameter-free tool names the rejected parameters, bounded, and a
// session request explains that a session takes no title.
func TestNoParamsNamesRejectedParameters(t *testing.T) {
	if err := noParams("session_begin", nil); err != nil {
		t.Fatalf("no arguments: %v", err)
	}
	err := noParams("session_begin", map[string]any{"title": "x"})
	if err == nil || !strings.Contains(err.Error(), `remove "title"`) || !strings.Contains(err.Error(), "a session takes no title") {
		t.Fatalf("session_begin title refusal: %v", err)
	}
	err = noParams("lint_llm_wiki", map[string]any{"b": 1, "a": 2})
	if err == nil || !strings.Contains(err.Error(), `remove "a", "b"`) || strings.Contains(err.Error(), "session") {
		t.Fatalf("sorted names, no session hint: %v", err)
	}
	many := map[string]any{strings.Repeat("k", 500): 1}
	for i := range 7 {
		many[fmt.Sprintf("p%d", i)] = i
	}
	err = noParams("session_list", many)
	if err == nil || !strings.Contains(err.Error(), "and 3 more") || strings.Contains(err.Error(), strings.Repeat("k", 65)) {
		t.Fatalf("bounded names: %v", err)
	}
}

func TestSessionBeginWithTitleIsRefusedWithGuidance(t *testing.T) {
	server, _ := newPublicWorkflowServer(t)
	for _, name := range []string{"session_begin", "begin_session"} {
		_, err := server.CallTool(context.Background(), name, map[string]any{"title": "Work on P-20"})
		if err == nil || !strings.Contains(err.Error(), `remove "title"`) || !strings.Contains(err.Error(), "keep the session_id it returns") {
			t.Fatalf("%s with a title: %v", name, err)
		}
	}
}
