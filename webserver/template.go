package webserver

import (
	"fmt"
	"html/template"
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
			"latency":             latency,
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

// Latency returns the latency between the two Times in human readable string.
func latency(t1, t2 *metav1.Time) string {
	if t1 == nil || t2 == nil {
		return "-"
	}
	return t2.Time.Sub(t1.Time).String()
}

// commitURL will return commit url from given repo url and commit hash
func commitURL(remoteURL, hash string) string {
	if remoteURL == "" {
		return ""
	}
	return fmt.Sprintf("https://%s/commit/%s", remoteURL, hash)
}
