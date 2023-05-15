package webserver

import (
	"fmt"
	"html/template"
	"strings"
	"time"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// createTemplate takes in a path to a template file and parses the file to create a Template instance.
func createTemplate(statusHTML string) (*template.Template, error) {
	tmpl, err := template.New("index").
		Funcs(template.FuncMap{
			"sanitizedUniqueName": sanitizedUniqueName,
			"commitURL":           commitURL,
			"formattedTime":       formattedTime,
			"duration":            duration,
		}).
		Parse(statusHTML)
	if err != nil {
		return nil, fmt.Errorf("error parsing template: %w", err)
	}
	return tmpl, nil
}

// sanitizedUniqueName will return namespaceName with - instead of /
func sanitizedUniqueName(m tfaplv1beta1.Module) string {
	return m.Namespace + "-" + m.Name
}

// FormattedTime returns the Time in the format "YYYY-MM-DD hh:mm:ss -0000 GMT"
func formattedTime(t *metav1.Time) string {
	if t == nil {
		return "not started"
	}
	return t.Time.Truncate(time.Second).Format(time.RFC3339)
}

// duration returns duration in human readable string.
func duration(d *metav1.Duration) string {
	if d == nil {
		return "-"
	}
	return d.Duration.Round(time.Second).String()
}

// commitURL will return commit url from given repo url and commit hash
func commitURL(remoteURL, hash string) string {
	if remoteURL == "" {
		return ""
	}
	remoteURL = strings.TrimSpace(remoteURL)
	remoteURL = strings.TrimPrefix(remoteURL, "ssh://")
	remoteURL = strings.TrimPrefix(remoteURL, "https://")
	remoteURL = strings.TrimPrefix(remoteURL, "git@")
	remoteURL = strings.TrimSuffix(remoteURL, ".git")
	remoteURL = strings.ReplaceAll(remoteURL, ":", "/")
	return fmt.Sprintf("https://%s/commit/%s", remoteURL, hash)
}
