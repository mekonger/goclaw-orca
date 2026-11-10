package providers

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers/acp"
)

func TestEstimateACPTokens(t *testing.T) {
	if got := estimateACPTokens(""); got != 0 {
		t.Fatalf("empty text: got %d, want 0", got)
	}
	if got := estimateACPTokens("hi"); got != 1 {
		t.Fatalf("short text: got %d, want 1 (floor)", got)
	}
	if got := estimateACPTokens("12345678"); got != 2 {
		t.Fatalf("8 chars: got %d, want 2", got)
	}
}

func TestACPContentText(t *testing.T) {
	blocks := []acp.ContentBlock{
		{Type: "text", Text: "hello "},
		{Type: "image", Data: "base64", MimeType: "image/png"},
		{Type: "text", Text: "world"},
	}
	if got := acpContentText(blocks); got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
}
