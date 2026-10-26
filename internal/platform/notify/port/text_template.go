package port

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// TextTemplate is plain text with {{variable_name}} substitutions. There are no
// expressions, functions, HTML or recursive expansion. The source owns allowed
// variables and must never supply PII. Notify still redacts before persistence.
type TextTemplate struct{ Title, Body string }

// Render rejects malformed/missing variables and overlong results atomically.
func (t TextTemplate) Render(values map[string]string) (title, body string, err error) {
	title, err = renderText(t.Title, values, 300)
	if err != nil {
		return "", "", err
	}
	body, err = renderText(t.Body, values, 2000)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(title) == "" {
		return "", "", fmt.Errorf("notify: template title is empty")
	}
	return title, body, nil
}

func renderText(text string, values map[string]string, limit int) (string, error) {
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > 4000 {
		return "", fmt.Errorf("notify: template is too long or invalid UTF-8")
	}
	var out strings.Builder
	for text != "" {
		start := strings.Index(text, "{{")
		if start < 0 {
			if strings.Contains(text, "}}") {
				return "", fmt.Errorf("notify: unmatched template braces")
			}
			out.WriteString(text)
			break
		}
		prefix := text[:start]
		if strings.Contains(prefix, "}}") {
			return "", fmt.Errorf("notify: unmatched template braces")
		}
		out.WriteString(prefix)
		text = text[start+2:]
		end := strings.Index(text, "}}")
		if end < 0 {
			return "", fmt.Errorf("notify: unmatched template braces")
		}
		key := strings.TrimSpace(text[:end])
		if !templateVariable(key) {
			return "", fmt.Errorf("notify: invalid template variable")
		}
		value, ok := values[key]
		if !ok {
			return "", fmt.Errorf("notify: missing template variable %s", key)
		}
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
			return "", fmt.Errorf("notify: template value is too long or invalid UTF-8")
		}
		out.WriteString(value)
		text = text[end+2:]
		if out.Len() > limit*utf8.UTFMax {
			return "", fmt.Errorf("notify: template result is too long")
		}
	}
	rendered := out.String()
	if utf8.RuneCountInString(rendered) > limit {
		return "", fmt.Errorf("notify: template result is too long")
	}
	return rendered, nil
}

func templateVariable(key string) bool {
	if len(key) == 0 || len(key) > 64 {
		return false
	}
	for i, c := range key {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}
