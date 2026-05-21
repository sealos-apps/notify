package render

import (
	"strings"
	"testing"

	"github.com/labring/sealos-notify/pkg/database"
)

func TestTemplateRendersSubjectAndBody(t *testing.T) {
	tpl := &database.Template{
		Name:    "incident-email",
		Subject: "[{{ .severity }}] {{ .incident }}",
		Body:    "Hello {{ .name }}, {{ .incident }} is {{ .status }}.",
	}

	got, err := Template(tpl, map[string]interface{}{
		"severity": "P0",
		"incident": "database",
		"name":     "Alice",
		"status":   "open",
	})
	if err != nil {
		t.Fatalf("Template returned error: %v", err)
	}

	if got.Subject != "[P0] database" {
		t.Fatalf("unexpected subject: %q", got.Subject)
	}
	if got.Body != "Hello Alice, database is open." {
		t.Fatalf("unexpected body: %q", got.Body)
	}
}

func TestTemplateMissingKeyRendersZeroValue(t *testing.T) {
	tpl := &database.Template{
		Name: "missing-key",
		Body: "Hello {{ .name }}",
	}

	got, err := Template(tpl, nil)
	if err != nil {
		t.Fatalf("Template returned error: %v", err)
	}
	if got.Body != "Hello <no value>" {
		t.Fatalf("unexpected body for missing key: %q", got.Body)
	}
}

func TestTemplateReturnsContextForParseErrors(t *testing.T) {
	tpl := &database.Template{
		Name: "broken",
		Body: "{{ .name ",
	}

	_, err := Template(tpl, map[string]interface{}{"name": "Alice"})
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), `render body for template "broken"`) {
		t.Fatalf("expected template name in error, got %v", err)
	}
}
