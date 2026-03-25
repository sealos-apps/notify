// Package render provides Go text/template rendering for notification templates
package render

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/labring/sealos-notify/pkg/database"
)

// Result holds the rendered subject and body
type Result struct {
	Subject string
	Body    string
}

// Template renders a database.Template using the provided params map.
// Both Subject and Body are rendered with Go text/template syntax.
// All params keys are available as top-level template variables (e.g. {{.name}}).
func Template(tpl *database.Template, params map[string]interface{}) (*Result, error) {
	result := &Result{}

	if tpl.Body != "" {
		b, err := renderString(tpl.Body, params)
		if err != nil {
			return nil, fmt.Errorf("render body for template %q: %w", tpl.Name, err)
		}
		result.Body = b
	}

	if tpl.Subject != "" {
		s, err := renderString(tpl.Subject, params)
		if err != nil {
			return nil, fmt.Errorf("render subject for template %q: %w", tpl.Name, err)
		}
		result.Subject = s
	}

	return result, nil
}

// renderString executes a Go template string with the given data map
func renderString(tmplStr string, data map[string]interface{}) (string, error) {
	t, err := template.New("").Option("missingkey=zero").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}
