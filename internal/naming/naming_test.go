package naming

import "testing"

func TestGenerateTitle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"single word", "refactor", "refactor"},
		{"preserves casing", "Fix the Login Bug", "Fix the Login Bug"},
		{"strips filler please", "please fix the tests", "fix the tests"},
		{"strips filler can you", "Can you add a button", "add a button"},
		{"strips filler lets", "let's refactor the auth module", "refactor the auth module"},
		{"case insensitive filler", "PLEASE update the README", "update the README"},
		{"slash command with text", "/commit fix the build", "fix the build"},
		{"slash command alone", "/commit", "commit"},
		{"multiline takes first line", "fix the bug\nsecond line\nthird line", "fix the bug"},
		{
			"truncation at word boundary",
			"implement the new authentication system with OAuth2 support and refresh tokens for all API endpoints",
			"implement the new authentication system with\u2026",
		},
		{
			"truncation preserves original casing",
			"Add comprehensive error handling for the database connection pooling layer with retries",
			"Add comprehensive error handling for the database\u2026",
		},
		{"unicode content", "修复登录问题", "修复登录问题"},
		{"filler then slash", "please /review the code", "/review the code"},
		{"only filler", "please ", "please"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateTitle(tt.input)
			if got != tt.want {
				t.Errorf("GenerateTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
