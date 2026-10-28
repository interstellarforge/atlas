package notifications

import (
	"net/mail"
	"strings"
	"testing"
)

func TestHTMLEscapesAndButtons(t *testing.T) {
	h := HTML("Hi <b>x</b>\n\nhttps://atlas.test/i/ATL-1\n\nbye")
	if strings.Contains(h, "<b>x</b>") || !strings.Contains(h, "&lt;b&gt;") {
		t.Fatal("body not escaped")
	}
	if !strings.Contains(h, `href="https://atlas.test/i/ATL-1"`) {
		t.Fatal("link paragraph is not a button")
	}
}

func TestBuildMessageIsMultipart(t *testing.T) {
	out := string(buildMessage(mail.Address{Address: "a@x.test"}, mail.Address{Address: "b@x.test"}, Message{Subject: "s", Body: "hello\n"}))
	for _, want := range []string{"multipart/alternative", "text/plain", "text/html", "--atlas-alt--"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q", want)
		}
	}
}
