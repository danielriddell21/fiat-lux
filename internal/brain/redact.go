package brain

import "strings"

func Redact(s string) string {
	// "sk-" covers both OpenAI keys and the common "sk-ant-" prefix for
	// Anthropic; matching one substring captures both.
	const prefix = "sk-"
	var out strings.Builder
	for {
		i := strings.Index(s, prefix)
		if i < 0 {
			out.WriteString(s)
			break
		}
		end := i + len(prefix)
		for end < len(s) && (isAlnum(s[end]) || s[end] == '-' || s[end] == '_') {
			end++
		}
		out.WriteString(s[:i])
		out.WriteString("[redacted]")
		s = s[end:]
	}
	return out.String()
}

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
