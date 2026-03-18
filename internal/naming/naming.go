package naming

import "strings"

const (
	maxTitleLen = 50
)

// fillerPrefixes are common conversational prefixes to strip from prompts.
var fillerPrefixes = []string{
	"please ",
	"can you ",
	"could you ",
	"i want you to ",
	"i need you to ",
	"i want to ",
	"i need to ",
	"i'd like you to ",
	"i'd like to ",
	"let's ",
	"go ahead and ",
	"hey ",
	"hi ",
	"hello ",
}

// GenerateTitle creates a short title from a user prompt using heuristics.
func GenerateTitle(prompt string) string {
	// Take first line only.
	if idx := strings.IndexByte(prompt, '\n'); idx != -1 {
		prompt = prompt[:idx]
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}

	// Strip leading slash commands (e.g. "/commit", "/review fix the bug").
	if len(prompt) > 0 && prompt[0] == '/' {
		if spaceIdx := strings.IndexByte(prompt, ' '); spaceIdx != -1 {
			prompt = strings.TrimSpace(prompt[spaceIdx+1:])
		} else {
			// Entire prompt is a slash command like "/commit" — use as-is.
			prompt = prompt[1:]
		}
	}

	// Strip filler prefixes (case-insensitive).
	lower := strings.ToLower(prompt)
	for _, prefix := range fillerPrefixes {
		if strings.HasPrefix(lower, prefix) {
			prompt = prompt[len(prefix):]
			lower = strings.ToLower(prompt)
			break
		}
	}
	prompt = strings.TrimSpace(prompt)

	if prompt == "" {
		return ""
	}

	// Truncate to ~maxTitleLen chars, breaking at word boundary.
	truncated := false
	if len(prompt) > maxTitleLen {
		cut := maxTitleLen
		for cut > maxTitleLen-15 && prompt[cut] != ' ' {
			cut--
		}
		if prompt[cut] == ' ' {
			prompt = prompt[:cut]
		} else {
			prompt = prompt[:maxTitleLen]
		}
		truncated = true
	}

	if truncated {
		prompt += "…"
	}

	return prompt
}

