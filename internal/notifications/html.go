package notifications

import (
	"html/template"
	"regexp"
	"strings"
)

var urlRe = regexp.MustCompile(`^https?://\S+$`)

var htmlTmpl = template.Must(template.New("mail").Parse(`<!doctype html>
<html><body style="margin:0;padding:24px;background:#f3f6fb;font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;color:#0b1a2e">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr><td align="center">
<table role="presentation" width="560" cellpadding="0" cellspacing="0" style="max-width:560px;width:100%;background:#ffffff;border:1px solid #cdd9e8;border-radius:12px">
<tr><td style="padding:20px 28px;border-bottom:1px solid #cdd9e8;font-size:18px;font-weight:700;color:#165dba">Atlas</td></tr>
<tr><td style="padding:24px 28px;font-size:15px;line-height:1.55">
{{range .}}{{if .Link}}<p style="margin:20px 0"><a href="{{.Text}}" style="display:inline-block;padding:10px 18px;background:#165dba;color:#ffffff;text-decoration:none;border-radius:8px;font-weight:600">{{.Label}}</a></p><p style="margin:0 0 16px;font-size:12px;color:#4c6a8b;word-break:break-all">{{.Text}}</p>{{else}}<p style="margin:0 0 16px">{{.Text}}</p>{{end}}{{end}}
</td></tr></table></td></tr></table></body></html>`))

type block struct {
	Text, Label string
	Link        bool
}

// HTML wraps a plain-text body in the Atlas mail layout: paragraphs become <p>, a paragraph that is only a URL becomes a button.
func HTML(body string) string {
	var blocks []block
	for _, p := range strings.Split(strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n")), "\n\n") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if urlRe.MatchString(p) {
			blocks = append(blocks, block{Text: p, Label: "Open in Atlas", Link: true})
			continue
		}
		blocks = append(blocks, block{Text: p})
	}
	var b strings.Builder
	if err := htmlTmpl.Execute(&b, blocks); err != nil {
		return ""
	}
	return b.String()
}
