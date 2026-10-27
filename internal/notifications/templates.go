package notifications

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates
var templateFS embed.FS

// Render fills the named template (templates/<lang>/<name>.txt) and splits it into subject and body.
// The first line must be "Subject: ...". Unknown languages fall back to English.
func Render(lang, name string, data any) (subject, body string, err error) {
	raw, err := templateFS.ReadFile(fmt.Sprintf("templates/%s/%s.txt", lang, name))
	if err != nil {
		if raw, err = templateFS.ReadFile(fmt.Sprintf("templates/en/%s.txt", name)); err != nil {
			return "", "", fmt.Errorf("unknown mail template %q", name)
		}
	}
	t, err := template.New(name).Parse(string(raw))
	if err != nil {
		return "", "", err
	}
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return "", "", err
	}
	head, rest, _ := strings.Cut(out.String(), "\n")
	subject, ok := strings.CutPrefix(head, "Subject: ")
	if !ok {
		return "", "", fmt.Errorf("mail template %q has no Subject line", name)
	}
	return strings.TrimSpace(subject), strings.TrimSpace(rest) + "\n", nil
}
