// Snippet: internal/format/output.go — output formatting + sanitization.
//
// Pipeline for Write:
//
//	parse JSON → (optionally unwrap envelope) → sanitize → filter fields
//	→ write as pretty JSON, NDJSON, or text
//
// Numbers are decoded with UseNumber() so large integer IDs survive the
// round-trip. Decoding into plain `any` would turn every number into a
// float64 and silently corrupt IDs beyond 2^53 on the way back out.
//
// A body that is not valid JSON is still sanitized before it reaches
// stdout — an API returning an HTML error page or text/plain is exactly
// the case sanitization exists for.
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
	"sort"
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

// Validate reports whether a --format value is one this CLI implements.
// Returning the canonical value keeps the caller from re-deriving it.
func Validate(f string) (string, error) {
	switch f {
	case "", "json":
		return "json", nil
	case "ndjson", "text":
		return f, nil
	default:
		return "", fmt.Errorf("unknown --format %q (want json, ndjson, or text)", f)
	}
}

// decodeJSON parses data preserving number literals exactly.
func decodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func Write(w io.Writer, data []byte, opts Options) error {
	v, err := decodeJSON(data)
	if err != nil {
		// Non-JSON passthrough — still sanitized, never raw.
		_, werr := io.WriteString(w, sanitizeString(string(data))+"\n")
		return werr
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
	case "text":
		return writeText(w, v)
	default:
		return writePrettyJSON(w, v)
	}
}

func DryRunOutput(w io.Writer, preview string) error { _, err := fmt.Fprintln(w, preview); return err }

// WriteLine emits exactly one compact JSON line for data, after the same
// sanitize + field-mask pipeline as Write. Used by the pagination walk so
// "one page per line" stays true even when the server pretty-prints, and
// so paged output is not the one path that skips sanitization.
func WriteLine(w io.Writer, data []byte, opts Options) error {
	v, err := decodeJSON(data)
	if err != nil {
		_, werr := io.WriteString(w, sanitizeString(strings.Join(strings.Fields(string(data)), " "))+"\n")
		return werr
	}
	v = sanitize(v)
	if len(opts.Fields) > 0 {
		v = filterFields(v, opts.Fields)
	}
	return writeJSONLine(w, v)
}

// writeText renders flat key/value lines for human reading. Deliberately
// lossy: agents should use the JSON formats. Nested values print as
// compact JSON so a line is never ambiguous.
func writeText(w io.Writer, v any) error {
	switch val := v.(type) {
	case []any:
		for i, elem := range val {
			if i > 0 {
				if _, err := io.WriteString(w, "\n"); err != nil {
					return err
				}
			}
			if err := writeText(w, elem); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, err := fmt.Fprintf(w, "%s\t%s\n", k, scalarText(val[k])); err != nil {
				return err
			}
		}
		return nil
	default:
		_, err := fmt.Fprintln(w, scalarText(v))
		return err
	}
}

func scalarText(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case nil:
		return ""
	case map[string]any, []any:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprint(val)
		}
		return string(b)
	default:
		return fmt.Sprint(val)
	}
}

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
//
// This stops accidents and copy-paste noise. It does NOT stop a motivated
// adversary — no string filter can, because the payload does not need a
// tag at all ("ignore previous instructions" is plain prose). The real
// mitigation is the calling agent treating CLI stdout as untrusted data.
// Keep that framing when you document this in the CLI's own skill.
var injectionTagPattern = regexp.MustCompile(`(?i)</?(system|assistant|human|user|thinking|tool_use|tool_result|function_calls|function_results|antml:[a-z_]*)[^>]*>`)

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

// SanitizeText applies the same stripping to a bare string. Exported so
// error paths outside this package (e.g. an API error body echoed to
// stderr) go through the same filter as stdout.
func SanitizeText(s string) string { return sanitizeString(s) }

func sanitizeString(s string) string {
	if s == "" {
		return s
	}
	// Loop to a fixpoint: a single pass lets "<sys<system>tem>" collapse
	// into the very tag it was supposed to remove.
	for {
		stripped := injectionTagPattern.ReplaceAllString(s, "")
		if stripped == s {
			break
		}
		s = stripped
	}
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
