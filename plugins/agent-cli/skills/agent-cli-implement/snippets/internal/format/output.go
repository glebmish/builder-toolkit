// Snippet: internal/format/output.go — output formatting + sanitization.
//
// Pipeline for Write:
//
//	parse JSON → (optionally unwrap envelope) → sanitize → filter fields
//	→ write as pretty JSON or NDJSON
//
// sanitize() and filterFields() are fully generic — no per-API knowledge.
// unwrapEnvelope is conditional: include only when docs/design.md says
// `Envelope: yes`.
package format

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type Options struct {
	Format string
	Fields []string
}

func FormatFromFlag(formatFlag, fieldsFlag string) Options {
	opts := Options{Format: formatFlag}
	if fieldsFlag != "" {
		for _, f := range strings.Split(fieldsFlag, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				opts.Fields = append(opts.Fields, f)
			}
		}
	}
	return opts
}

func Write(w io.Writer, data []byte, opts Options) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		_, err := w.Write(append(data, '\n')) // non-JSON passthrough
		return err
	}
	// If design.md says Envelope: yes, uncomment:
	// v = unwrapEnvelope(v, "data", true)
	v = sanitize(v)
	if len(opts.Fields) > 0 {
		v = filterFields(v, opts.Fields)
	}
	switch opts.Format {
	case "ndjson":
		return writeNDJSON(w, v)
	default:
		return writePrettyJSON(w, v)
	}
}

func WriteRaw(w io.Writer, data []byte) error        { _, err := w.Write(data); return err }
func DryRunOutput(w io.Writer, preview string) error { _, err := fmt.Fprintln(w, preview); return err }

func writePrettyJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func writeNDJSON(w io.Writer, v any) error {
	arr, ok := v.([]any)
	if !ok {
		return writeJSONLine(w, v)
	}
	for _, elem := range arr {
		if err := writeJSONLine(w, elem); err != nil {
			return err
		}
	}
	return nil
}

func writeJSONLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.Write(b)
	buf.WriteByte('\n')
	_, err = w.Write(buf.Bytes())
	return err
}

// ---- Envelope unwrap (conditional) ---------------------------------------

// unwrapEnvelope strips a single-key wrapper like {"data": <real>}. Set
// recursive=true to also peel a single-key inner wrapper (per-resource
// re-namespacing). Include only when design.md says Envelope: yes.
func unwrapEnvelope(v any, key string, recursive bool) any {
	for {
		m, ok := v.(map[string]any)
		if !ok {
			return v
		}
		inner, ok := m[key]
		if !ok || len(m) != 1 {
			return v
		}
		v = inner
		if !recursive {
			return v
		}
		nested, ok := v.(map[string]any)
		if !ok || len(nested) != 1 {
			return v
		}
		for _, vv := range nested {
			v = vv
		}
	}
}

// ---- Sanitization (always on) --------------------------------------------

// injectionTagPattern matches XML-ish tag wrappers used in some prompt-
// injection attempts. Strip the wrapper, keep inner text.
var injectionTagPattern = regexp.MustCompile(`(?i)</?(system|assistant|tool_use|tool_result)[^>]*>`)

func sanitize(v any) any {
	switch val := v.(type) {
	case string:
		return sanitizeString(val)
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[k] = sanitize(child)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			out[i] = sanitize(child)
		}
		return out
	default:
		return v
	}
}

func sanitizeString(s string) string {
	if s == "" {
		return s
	}
	s = injectionTagPattern.ReplaceAllString(s, "")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ---- Field mask ---------------------------------------------------------

// filterFields keeps only the requested dotted paths. Builds a trie so a
// single walk handles arbitrary depth, descending into arrays implicitly:
// `rows.id` filters {"rows":[{"id":1,"x":2}]} → {"rows":[{"id":1}]}.
func filterFields(v any, fields []string) any {
	type node struct {
		leaf     bool
		children map[string]*node
	}
	root := &node{children: map[string]*node{}}
	for _, f := range fields {
		cur := root
		for _, seg := range strings.Split(f, ".") {
			child, ok := cur.children[seg]
			if !ok {
				child = &node{children: map[string]*node{}}
				cur.children[seg] = child
			}
			cur = child
		}
		cur.leaf = true
	}
	var walk func(v any, n *node) any
	walk = func(v any, n *node) any {
		if n == nil {
			return v
		}
		switch val := v.(type) {
		case map[string]any:
			out := make(map[string]any)
			for k, child := range val {
				cn, ok := n.children[k]
				if !ok {
					continue
				}
				if cn.leaf && len(cn.children) == 0 {
					out[k] = child
				} else {
					out[k] = walk(child, cn)
				}
			}
			return out
		case []any:
			out := make([]any, len(val))
			for i, elem := range val {
				out[i] = walk(elem, n)
			}
			return out
		default:
			return v
		}
	}
	return walk(v, root)
}
