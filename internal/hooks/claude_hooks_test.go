package hooks

import (
	"encoding/json"
	"testing"
)

func TestMergeHookEvent(t *testing.T) {
	t.Run("nil input creates new matcher", func(t *testing.T) {
		result := mergeHookEvent(nil, "", true)
		if result == nil {
			t.Fatal("expected non-nil result")
		}

		var matchers []claudeHookMatcher
		if err := json.Unmarshal(result, &matchers); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(matchers) != 1 {
			t.Fatalf("expected 1 matcher, got %d", len(matchers))
		}
		if len(matchers[0].Hooks) != 1 {
			t.Fatalf("expected 1 hook, got %d", len(matchers[0].Hooks))
		}
		if matchers[0].Hooks[0].Type != "command" {
			t.Errorf("hook type = %q, want command", matchers[0].Hooks[0].Type)
		}
	})

	t.Run("appends to existing hooks", func(t *testing.T) {
		existing := json.RawMessage(`[{"hooks":[{"type":"command","command":"other-tool"}]}]`)
		result := mergeHookEvent(existing, "", true)

		var matchers []claudeHookMatcher
		if err := json.Unmarshal(result, &matchers); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(matchers) != 1 {
			t.Fatalf("expected 1 matcher, got %d", len(matchers))
		}
		if len(matchers[0].Hooks) != 2 {
			t.Fatalf("expected 2 hooks, got %d", len(matchers[0].Hooks))
		}
	})

	t.Run("updates existing brizz-code hook", func(t *testing.T) {
		existing := json.RawMessage(`[{"hooks":[{"type":"command","command":"brizz-code hook-handler"}]}]`)
		result := mergeHookEvent(existing, "", true)

		var matchers []claudeHookMatcher
		if err := json.Unmarshal(result, &matchers); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		// Should not duplicate.
		if len(matchers[0].Hooks) != 1 {
			t.Fatalf("expected 1 hook (no duplicate), got %d", len(matchers[0].Hooks))
		}
	})

	t.Run("respects matcher", func(t *testing.T) {
		existing := json.RawMessage(`[{"matcher":"other","hooks":[{"type":"command","command":"other"}]}]`)
		result := mergeHookEvent(existing, "permission_prompt", true)

		var matchers []claudeHookMatcher
		if err := json.Unmarshal(result, &matchers); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(matchers) != 2 {
			t.Fatalf("expected 2 matchers, got %d", len(matchers))
		}
	})
}

func TestRemoveBrizzCodeFromEvent(t *testing.T) {
	t.Run("only brizz-code hooks", func(t *testing.T) {
		raw := json.RawMessage(`[{"hooks":[{"type":"command","command":"brizz-code hook-handler"}]}]`)
		result, removed := removeBrizzCodeFromEvent(raw)
		if !removed {
			t.Error("expected removed=true")
		}
		if result != nil {
			t.Error("expected nil result when all hooks removed")
		}
	})

	t.Run("mixed hooks", func(t *testing.T) {
		raw := json.RawMessage(`[{"hooks":[{"type":"command","command":"other-tool"},{"type":"command","command":"brizz-code hook-handler"}]}]`)
		result, removed := removeBrizzCodeFromEvent(raw)
		if !removed {
			t.Error("expected removed=true")
		}
		if result == nil {
			t.Fatal("expected non-nil result (other hooks remain)")
		}

		var matchers []claudeHookMatcher
		if err := json.Unmarshal(result, &matchers); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(matchers[0].Hooks) != 1 {
			t.Fatalf("expected 1 remaining hook, got %d", len(matchers[0].Hooks))
		}
		if matchers[0].Hooks[0].Command != "other-tool" {
			t.Errorf("remaining hook = %q, want other-tool", matchers[0].Hooks[0].Command)
		}
	})

	t.Run("no brizz-code hooks", func(t *testing.T) {
		raw := json.RawMessage(`[{"hooks":[{"type":"command","command":"other-tool"}]}]`)
		_, removed := removeBrizzCodeFromEvent(raw)
		if removed {
			t.Error("expected removed=false")
		}
	})
}
