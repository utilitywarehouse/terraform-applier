package sysutil

import (
	"fmt"
	"strings"
	"text/template"
)

// CreateTemplate takes in a path to a template file and parses the file to create a Template instance.
func CreateTemplate(status string) (*template.Template, error) {
	tmpl, err := template.New("index").
		Funcs(template.FuncMap{
			"sanitizeString": sanitizeString,
			"splitByNewline": splitByNewline,
			"getOutputClass": getOutputClass,
		}).
		Parse(status)
	if err != nil {
		return nil, fmt.Errorf("Error parsing template: %v", err)
	}
	return tmpl, nil
}

// sanitizeString will remove all `/` from module path
func sanitizeString(str string) string {
	return strings.ReplaceAll(str, "/", "_")
}

func splitByNewline(output string) []string {
	return strings.Split(output, "\n")
}

// getOutputClass will return classes for text color based on prefix of output str
func getOutputClass(l string) string {
	l = strings.TrimSpace(l)

	if strings.HasPrefix(l, "+") {
		return "text-success"
	}
	if strings.HasPrefix(l, "~") {
		return "text-warning"
	}
	if strings.HasPrefix(l, "-") &&
		!strings.HasPrefix(l, "- Finding") &&
		!strings.HasPrefix(l, "- Instal") {
		return "text-danger"
	}

	if strings.Contains(l, "Plan:") {
		return "text-primary"
	}
	if strings.Contains(l, "No changes.") {
		return "text-primary"
	}
	return ""
}
