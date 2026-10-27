package repo

import (
	"bytes"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// imageHosts are hosts, besides the repository's own, whose images a README may show through Atlas's image
// proxy: badges and the providers' user-content hosts. Anything else stays a plain link.
var imageHosts = []string{"img.shields.io", "shields.io", "badgen.net", "github.com", "gitlab.com", "codeberg.org", "bitbucket.org"}

// ImageHostAllowed says whether an image from host may be fetched for a README of r.
func (r Repo) ImageHostAllowed(host string) bool {
	host = strings.ToLower(host)
	if host == r.Hostname() || host == r.RawHost() || strings.HasSuffix(host, ".githubusercontent.com") {
		return true
	}
	for _, h := range imageHosts {
		if host == h {
			return true
		}
	}
	return false
}

// Rewriter turns README markdown into markdown Atlas can render with raw HTML off: relative paths point at the
// repository, images go through Asset (the image proxy) and the HTML that READMEs commonly use for layout is
// reduced to its markdown equivalent.
type Rewriter struct {
	Repo Repo
	// Asset maps an absolute https image URL to the URL the browser should load it from.
	Asset func(abs string) string
}

var (
	fenceRe       = regexp.MustCompile("(?m)^(```|~~~)")
	commentRe     = regexp.MustCompile(`(?s)<!--.*?-->`)
	imgTagRe      = regexp.MustCompile(`(?i)<img\b[^>]*>`)
	attrRe        = regexp.MustCompile(`(?i)([a-z-]+)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	aTagRe        = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	hTagRe        = regexp.MustCompile(`(?is)<h([1-6])\b[^>]*>(.*?)</h[1-6]>`)
	brRe          = regexp.MustCompile(`(?i)<br\s*/?>`)
	boldRe        = regexp.MustCompile(`(?is)<(?:b|strong)>(.*?)</(?:b|strong)>`)
	italRe        = regexp.MustCompile(`(?is)<(?:i|em)>(.*?)</(?:i|em)>`)
	wrapRe        = regexp.MustCompile(`(?i)</?(?:p|div|center|picture|source|span|details|summary)\b[^>]*>`)
	linkRe        = regexp.MustCompile(`(!?)\[((?:[^\[\]]|\[[^\]]*\])*)\]\(\s*<?([^)\s>]+)>?((?:\s+"[^"]*")?)\s*\)`)
	indentedImgRe = regexp.MustCompile(`(?m)^[ \t]+(\[?!\[)`)
	refDefRe      = regexp.MustCompile(`(?m)^(\s{0,3}\[[^\]]+\]:\s*)(\S+)`)
)

// Rewrite returns the prepared markdown. Fenced code blocks are left alone.
func (rw Rewriter) Rewrite(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	var out strings.Builder
	var seg strings.Builder
	inFence := false
	flush := func() {
		if seg.Len() == 0 {
			return
		}
		if inFence {
			out.WriteString(seg.String())
		} else {
			out.WriteString(rw.prose(seg.String()))
		}
		seg.Reset()
	}
	for _, line := range strings.SplitAfter(md, "\n") {
		if fenceRe.MatchString(line) {
			if !inFence { // the opening fence belongs to the code segment
				flush()
				inFence = true
				seg.WriteString(line)
				continue
			}
			seg.WriteString(line) // the closing fence ends it
			flush()
			inFence = false
			continue
		}
		seg.WriteString(line)
	}
	flush()
	return out.String()
}

func (rw Rewriter) prose(s string) string {
	s = commentRe.ReplaceAllString(s, "")
	s = imgTagRe.ReplaceAllStringFunc(s, func(tag string) string {
		a := attrs(tag)
		if a["src"] == "" {
			return ""
		}
		return "![" + escapeLabel(a["alt"]) + "](" + a["src"] + ")"
	})
	s = aTagRe.ReplaceAllStringFunc(s, func(tag string) string {
		m := aTagRe.FindStringSubmatch(tag)
		href := attrs("<a " + m[1] + ">")["href"]
		inner := strings.TrimSpace(m[2])
		if href == "" {
			return inner
		}
		return "[" + inner + "](" + href + ")"
	})
	s = hTagRe.ReplaceAllStringFunc(s, func(tag string) string {
		m := hTagRe.FindStringSubmatch(tag)
		return "\n" + strings.Repeat("#", int(m[1][0]-'0')) + " " + strings.TrimSpace(m[2]) + "\n"
	})
	s = brRe.ReplaceAllString(s, "\n")
	s = boldRe.ReplaceAllString(s, "**$1**")
	s = italRe.ReplaceAllString(s, "*$1*")
	s = wrapRe.ReplaceAllString(s, "")
	// Layout HTML is usually indented; once its tags are gone the indent would turn an image line into a code block.
	s = indentedImgRe.ReplaceAllString(s, "$1")
	s = rw.links(s)
	return refDefRe.ReplaceAllStringFunc(s, func(def string) string {
		m := refDefRe.FindStringSubmatch(def)
		return m[1] + rw.linkTarget(m[2])
	})
}

func attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
		v := m[2]
		if v == "" {
			v = m[3]
		}
		out[strings.ToLower(m[1])] = strings.TrimSpace(v)
	}
	return out
}

func escapeLabel(s string) string {
	return strings.NewReplacer("[", "(", "]", ")", "\n", " ").Replace(strings.TrimSpace(s))
}

func (rw Rewriter) links(s string) string {
	return linkRe.ReplaceAllStringFunc(s, func(m string) string {
		p := linkRe.FindStringSubmatch(m)
		bang, label, target, title := p[1], p[2], p[3], p[4]
		if bang == "!" {
			return rw.image(label, target)
		}
		label = rw.links(label) // [![badge](img)](link)
		t := rw.linkTarget(target)
		if t == "" {
			return label // a scheme we do not link to (javascript:, data:, file:)
		}
		return "[" + label + "](" + t + title + ")"
	})
}

func (rw Rewriter) image(alt, target string) string {
	abs, ok := rw.absolute(target, true)
	if !ok {
		return alt
	}
	u, err := url.Parse(abs)
	if err != nil || !rw.Repo.ImageHostAllowed(u.Hostname()) || rw.Asset == nil {
		if alt == "" {
			alt = u.Hostname()
		}
		return "[" + alt + "](" + abs + ")" // not shown inline, still reachable
	}
	return "![" + alt + "](" + rw.Asset(abs) + ")"
}

// linkTarget resolves a link: relative paths point at the repository's web page, other schemes are dropped.
func (rw Rewriter) linkTarget(t string) string {
	t = strings.TrimSpace(t)
	if strings.HasPrefix(t, "#") {
		return t
	}
	if abs, ok := rw.absolute(t, false); ok {
		return abs
	}
	return ""
}

// absolute resolves t against the repository. forRaw selects raw file URLs (images) over web pages (links).
func (rw Rewriter) absolute(t string, forRaw bool) (string, bool) {
	t = strings.TrimSpace(t)
	lower := strings.ToLower(t)
	switch {
	case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"):
		if strings.HasPrefix(lower, "http://") {
			t = "https://" + t[len("http://"):] // images and pages are fetched over https only
		}
		return t, true
	case strings.HasPrefix(lower, "mailto:") && !forRaw:
		return t, true
	case strings.HasPrefix(t, "//"):
		return "https:" + t, true
	case strings.Contains(strings.SplitN(t, "/", 2)[0], ":"): // javascript:, data:, file:, ...
		return "", false
	}
	frag := ""
	if i := strings.IndexAny(t, "#?"); i >= 0 {
		t, frag = t[:i], t[i:]
		if forRaw {
			frag = ""
		}
	}
	file := cleanRel(t)
	if forRaw {
		return rw.Repo.RawURL(file), true
	}
	if file == "" {
		return rw.Repo.URL + frag, true
	}
	return rw.Repo.BlobURL(file) + frag, true
}

// ImageType decides the content type to serve an image under, or "" if the data is not a safe image. The
// declared type is only trusted for the raster formats; SVG is recognised by content and served as an
// image/svg+xml document that can only be shown through <img> (with a sandboxing CSP).
func ImageType(declared string, body []byte) string {
	switch declared {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		if got := http.DetectContentType(body); got == declared {
			return declared
		}
		return ""
	}
	head := body
	if len(head) > 2048 {
		head = head[:2048]
	}
	if bytes.Contains(bytes.ToLower(head), []byte("<svg")) {
		return "image/svg+xml"
	}
	switch got := http.DetectContentType(body); got {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return got
	}
	return ""
}
