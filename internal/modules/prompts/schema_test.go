package prompts

import (
	"strings"
	"testing"
)

func chatSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"x-aihub-variables": []any{
			map[string]any{"name": "topic", "description": "主题"},
		},
		"properties": map[string]any{
			"title": map[string]any{"type": "string", "x-aihub-ui": "text", "maxLength": float64(10)},
			"count": map[string]any{"type": "integer", "minimum": float64(1), "maximum": float64(5)},
			"messages": map[string]any{
				"type": "array", "x-aihub-ui": "repeatable-group",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"role": map[string]any{"type": "string", "enum": []any{"user", "assistant"}},
						"body": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}

func TestValidateSchemaOK(t *testing.T) {
	if err := ValidateSchema(chatSchema()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSchemaRejectsUnknownUI(t *testing.T) {
	s := chatSchema()
	s["properties"].(map[string]any)["weird"] = map[string]any{"type": "string", "x-aihub-ui": "javascript"}
	if err := ValidateSchema(s); err == nil {
		t.Fatal("expected error for unknown ui")
	}
}

func TestValidateContent(t *testing.T) {
	s := chatSchema()
	ok := map[string]any{
		"title": "你好",
		"count": float64(3),
		"messages": []any{
			map[string]any{"role": "user", "body": "hi"},
		},
	}
	if err := ValidateContent(s, ok); err != nil {
		t.Fatal(err)
	}
	bad := map[string]any{"title": "this is way too long"}
	if err := ValidateContent(s, bad); err == nil {
		t.Fatal("expected maxLength error")
	}
	bad2 := map[string]any{"messages": []any{map[string]any{"role": "other"}}}
	if err := ValidateContent(s, bad2); err == nil {
		t.Fatal("expected enum error")
	}
}

func TestValidateContentEnforcesTopLevelRequired(t *testing.T) {
	s := map[string]any{
		"type":     "object",
		"required": []any{"title", "messages"},
		"properties": map[string]any{
			"title":    map[string]any{"type": "string"},
			"messages": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	if err := ValidateContent(s, map[string]any{"title": "x"}); err == nil {
		t.Fatal("expected missing required field error")
	}
	if err := ValidateContent(s, map[string]any{"title": "x", "messages": []any{"a"}}); err != nil {
		t.Fatal(err)
	}
	// nested repeatable-group required array
	s2 := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":     "object",
					"required": []any{"role"},
					"properties": map[string]any{
						"role": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	if err := ValidateContent(s2, map[string]any{"items": []any{map[string]any{}}}); err == nil {
		t.Fatal("expected nested required error")
	}
}

func TestValidateVariables(t *testing.T) {
	s := chatSchema()
	content := map[string]any{"title": "关于 {{topic}} 的讨论"}
	used, err := ValidateVariables(s, content)
	if err != nil {
		t.Fatal(err)
	}
	if len(used) != 1 || used[0] != "topic" {
		t.Fatalf("used = %v", used)
	}
	bad := map[string]any{"title": "关于 {{undefined_var}} 的讨论"}
	if _, err := ValidateVariables(s, bad); err == nil {
		t.Fatal("expected undeclared variable error")
	}
}

func TestRenderTemplate(t *testing.T) {
	out := RenderTemplate("你好 {{name}}，主题：{{topic}}", map[string]any{"name": "AI", "topic": "Go"})
	if !strings.Contains(out, "AI") || !strings.Contains(out, "Go") {
		t.Fatalf("render = %q", out)
	}
	if RenderTemplate("{{missing}}", map[string]any{}) != "{{missing}}" {
		t.Fatal("unresolved variable should stay literal")
	}
}
