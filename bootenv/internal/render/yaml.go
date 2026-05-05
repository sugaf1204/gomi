package render

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type yamlWriter struct {
	b strings.Builder
}

func (w *yamlWriter) line(indent int, format string, args ...any) {
	w.b.WriteString(strings.Repeat(" ", indent))
	w.b.WriteString(fmt.Sprintf(format, args...))
	w.b.WriteByte('\n')
}

func (w *yamlWriter) string() string {
	return w.b.String()
}

func yamlString(s string) string {
	if s == "" {
		return `""`
	}
	return strconv.Quote(s)
}

func stringList(w *yamlWriter, indent int, key string, values []string) {
	if len(values) == 0 {
		return
	}
	w.line(indent, "%s:", key)
	for _, value := range values {
		w.line(indent+2, "- %s", yamlString(value))
	}
}

func envList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func blockScalar(w *yamlWriter, indent int, key string, value string) {
	w.line(indent, "%s: |", key)
	if value == "" {
		w.line(indent+2, "")
		return
	}
	for _, line := range strings.Split(strings.TrimRight(value, "\n"), "\n") {
		w.line(indent+2, "%s", line)
	}
}
