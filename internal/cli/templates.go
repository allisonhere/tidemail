package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/mailtemplate"
)

// loadTemplates returns the built-ins plus the user's template files.
func loadTemplates() []mailtemplate.Template {
	dir, _ := config.TemplatesDir()
	return mailtemplate.Load(dir)
}

// findTemplate looks a template up by name, ignoring case.
func findTemplate(name string) (mailtemplate.Template, bool) {
	name = strings.TrimSpace(name)
	for _, t := range loadTemplates() {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return mailtemplate.Template{}, false
}

// runTemplates implements "tidemail templates list [--json]".
func runTemplates(args []string, out, errOut io.Writer) int {
	j := false
	var rest []string
	for _, a := range args {
		if a == "--json" || a == "--json=true" {
			j = true
		} else {
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 || rest[0] != "list" || len(rest) > 1 {
		usage := "Usage: tidemail templates list [--json]\nTemplates fill a message body: tidemail send --template NAME --to ADDRESS."
		if hasHelpFlag(args) || len(args) == 0 {
			_, _ = fmt.Fprintln(out, usage)
			return 0
		}
		_, _ = fmt.Fprintln(errOut, usage)
		return 2
	}
	list := loadTemplates()
	if !j {
		for _, t := range list {
			line := t.Name
			if t.Subject != "" {
				line += "\t" + t.Subject
			}
			if _, err := fmt.Fprintln(out, line); err != nil {
				return 1
			}
		}
		return 0
	}
	items := make([]map[string]string, 0, len(list))
	for _, t := range list {
		items = append(items, map[string]string{"name": t.Name, "subject": t.Subject})
	}
	return mailResult(out, errOut, true, map[string]any{"templates": items}, 0, "", "")
}
