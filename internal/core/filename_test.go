package core

import (
	"strings"
	"testing"
)

func TestCleanFilenameSurvivesHugeExtension(t *testing.T) {
	got := cleanFilename("a." + strings.Repeat("x", 500))
	if len(got) > maxFilenameLen {
		t.Fatalf("name is %d bytes", len(got))
	}
}
