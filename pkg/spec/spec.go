// Package spec is humagen's configuration language: versioned documents of two
// kinds, each with one owner. A System document says how and where humagen runs
// (listeners, sign-in, stores, bus, files, limits, extensions) and belongs to
// whoever runs the process. A Platform document says what one workspace
// offers (its models, tools, agents, surfaces and settings) and belongs to
// that workspace's admins. docs/spec.md is the reference.
//
// Four rules hold for every field:
//
//   - Every field has a working default, so no file is a running platform.
//   - A secret is a reference (Ref), never a value.
//   - Every replaceable part is a Slot: "kind" plus a block named after it.
//     Built-in kinds are checked here; a binary registers more, and the file
//     selects them by name.
//   - Unknown fields are errors, reported with their path.
//
// The package only reads, checks and writes documents. It builds nothing:
// internal/app turns a Config into a running platform.
package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/credref"
)

// APIVersion is the version of the language this package reads.
const APIVersion = "humagen/v1"

// Document kinds.
const (
	KindSystem   = "System"
	KindPlatform = "Platform"
)

// ErrInvalid reports a document that does not follow the language.
var ErrInvalid = errors.New("spec: invalid")

// Config is every document read, merged: at most one System, and one
// Platform per workspace.
type Config struct {
	System    System     `json:"system"`
	Platforms []Platform `json:"platforms,omitempty"`
	// Notes are what the files said under a name from before the rename,
	// which still works: one line each, for the person at start.
	Notes []string `json:"-"`
}

// Metadata names a document's subject.
type Metadata struct {
	// Name is an identifier: the deployment's for a System, the tenant for a
	// Platform.
	Name string `json:"name,omitempty"`
	// DisplayName is what people see.
	DisplayName string `json:"display_name,omitempty"`
}

// header is what every document starts with.
type header struct {
	APIVersion string          `json:"api_version"`
	Kind       string          `json:"kind"`
	Metadata   Metadata        `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

// Ref names where a secret is kept. It is written {ref: env:NAME}, so a bare
// string, which would be the secret itself, is refused.
type Ref struct {
	Ref string `json:"ref"`
}

// UnmarshalJSON refuses a bare string and an unknown scheme.
func (r *Ref) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return errors.New("a secret is a reference, {ref: env:NAME}, never a value")
	}
	var raw struct {
		Ref string `json:"ref"`
	}
	if err := strictJSON(b, &raw); err != nil {
		return err
	}
	if _, err := credref.Parse(raw.Ref); err != nil {
		return err
	}
	r.Ref = raw.Ref
	return nil
}

// Set reports whether the reference names something.
func (r *Ref) Set() bool { return r != nil && r.Ref != "" }

// Slot is a replaceable part: a kind, and the block named after it, kept raw
// until the kind's owner decodes it.
type Slot struct {
	Kind  string
	Block json.RawMessage
}

// Decode reads the block into v strictly. A missing block is an empty one.
func (s Slot) Decode(v any) error {
	if len(s.Block) == 0 || string(s.Block) == "null" {
		return nil
	}
	return strictJSON(s.Block, v)
}

// MarshalJSON writes the slot as it is read.
func (s Slot) MarshalJSON() ([]byte, error) { return marshalSlot(s, nil) }

// UnmarshalJSON reads a slot with no shared fields.
func (s *Slot) UnmarshalJSON(b []byte) error { return unmarshalSlot(b, s, nil) }

// unmarshalSlot splits an object into its kind, the block named after it,
// and the rest, which is decoded strictly into shared (nil means none).
func unmarshalSlot(b []byte, s *Slot, shared any) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	kind := ""
	if k, ok := m["kind"]; ok {
		if err := json.Unmarshal(k, &kind); err != nil {
			return fmt.Errorf("kind: %w", err)
		}
		delete(m, "kind")
	}
	s.Kind = kind
	if kind != "" {
		s.Block = m[kind]
		delete(m, kind)
	}
	if len(m) == 0 {
		return nil
	}
	if shared == nil {
		return fmt.Errorf("unknown field %q", firstKey(m))
	}
	rest, _ := json.Marshal(m)
	return strictJSON(rest, shared)
}

// marshalSlot writes a slot as it is read: a name first if it has one, then
// the kind and its block, then the other shared fields in declaration order.
func marshalSlot(s Slot, shared any) ([]byte, error) {
	var fields []field
	if shared != nil {
		b, err := json.Marshal(shared)
		if err != nil {
			return nil, err
		}
		if fields, err = orderedFields(b); err != nil {
			return nil, err
		}
	}
	var out []field
	rest := fields
	if len(fields) > 0 && fields[0].key == "name" {
		out, rest = append(out, fields[0]), fields[1:]
	}
	if s.Kind != "" {
		k, _ := json.Marshal(s.Kind)
		out = append(out, field{"kind", k})
		if len(s.Block) > 0 && string(s.Block) != "null" {
			out = append(out, field{s.Kind, s.Block})
		}
	}
	out = append(out, rest...)
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range out {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(f.key)
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(f.value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

type field struct {
	key   string
	value json.RawMessage
}

// orderedFields reads a JSON object's fields in the order they are written.
func orderedFields(b []byte) ([]field, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	var out []field
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, field{k.(string), v})
	}
	return out, nil
}

func firstKey(m map[string]json.RawMessage) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys[0]
}

// strictJSON decodes b into v, refusing unknown fields and trailing data.
func strictJSON(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

// Load reads every document in paths. A directory reads its *.yaml, *.yml
// and *.json files in name order. Documents of the same kind and name are an
// error, not a merge, so two files cannot quietly disagree.
func Load(paths ...string) (Config, error) {
	var cfg Config
	var files []string
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return cfg, fmt.Errorf("spec: %w", err)
		}
		if !st.IsDir() {
			files = append(files, p)
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return cfg, fmt.Errorf("spec: %w", err)
		}
		for _, e := range entries {
			switch filepath.Ext(e.Name()) {
			case ".yaml", ".yml", ".json":
				if !e.IsDir() {
					files = append(files, filepath.Join(p, e.Name()))
				}
			}
		}
	}
	haveSystem := false
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return cfg, fmt.Errorf("spec: %w", err)
		}
		docs, err := Parse(b)
		if err != nil {
			return cfg, fmt.Errorf("%s: %w", f, err)
		}
		for _, n := range docs.Notes {
			cfg.Notes = append(cfg.Notes, f+": "+n)
		}
		if docs.hasSystem {
			if haveSystem {
				return cfg, fmt.Errorf("%s: %w: a second System document", f, ErrInvalid)
			}
			haveSystem, cfg.System = true, docs.System
		}
		for _, p := range docs.Platforms {
			if slices.ContainsFunc(cfg.Platforms, func(q Platform) bool { return q.Metadata.Name == p.Metadata.Name }) {
				return cfg, fmt.Errorf("%s: %w: a second Platform named %q", f, ErrInvalid, p.Metadata.Name)
			}
			cfg.Platforms = append(cfg.Platforms, p)
		}
	}
	return cfg, cfg.check()
}

// parsed is one file's documents.
type parsed struct {
	Config
	hasSystem bool
}

// Parse reads the documents in one YAML or JSON stream.
func Parse(b []byte) (parsed, error) {
	var out parsed
	dec := yaml.NewDecoder(bytes.NewReader(b))
	for i := 1; ; i++ {
		var node any
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out, fmt.Errorf("document %d: %w", i, err)
		}
		if node == nil {
			continue
		}
		raw, err := json.Marshal(node)
		if err != nil {
			return out, fmt.Errorf("document %d: %w", i, err)
		}
		var h header
		if err := strictJSON(raw, &h); err != nil {
			return out, fmt.Errorf("document %d: %w", i, err)
		}
		switch h.APIVersion {
		case APIVersion:
		case legacy.APIVersion:
			out.Notes = append(out.Notes, fmt.Sprintf("document %d: api_version %s still works; write %s", i, legacy.APIVersion, APIVersion))
		default:
			return out, fmt.Errorf("document %d: %w: api_version is %q, want %q", i, ErrInvalid, h.APIVersion, APIVersion)
		}
		where := fmt.Sprintf("document %d (%s %s)", i, h.Kind, h.Metadata.Name)
		switch h.Kind {
		case KindSystem:
			if out.hasSystem {
				return out, fmt.Errorf("%s: %w: a second System document", where, ErrInvalid)
			}
			out.hasSystem = true
			out.System.Metadata = h.Metadata
			if err := decodeSpec(h.Spec, &out.System.Spec); err != nil {
				return out, fmt.Errorf("%s: spec: %w", where, err)
			}
		case KindPlatform:
			p := Platform{Metadata: h.Metadata}
			if err := decodeSpec(h.Spec, &p.Spec); err != nil {
				return out, fmt.Errorf("%s: spec: %w", where, err)
			}
			notes, err := p.Spec.Surfaces.Slack.adoptOld()
			if err != nil {
				return out, fmt.Errorf("%s: spec: %w", where, err)
			}
			for _, n := range notes {
				out.Notes = append(out.Notes, where+": "+n)
			}
			out.Platforms = append(out.Platforms, p)
		default:
			return out, fmt.Errorf("document %d: %w: kind is %s or %s, not %q", i, ErrInvalid, KindSystem, KindPlatform, h.Kind)
		}
	}
	return out, nil
}

func decodeSpec(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return strictJSON(raw, v)
}

// Marshal writes a config as YAML documents, System first, in the order the
// fields are declared, so a file humagen writes reads like one a person wrote.
func Marshal(cfg Config) ([]byte, error) { return marshal(cfg, APIVersion) }

// MarshalOld is Marshal in the language as it was before the rename
// (api_version warp/v1, surfaces.slack.warp_client and warp_token), which
// this package still reads. setup writes a spec it found under its old name
// this way, so the release before the rename can still read it.
func MarshalOld(cfg Config) ([]byte, error) {
	out := cfg
	out.Platforms = make([]Platform, len(cfg.Platforms))
	for i, p := range cfg.Platforms {
		if s := p.Spec.Surfaces.Slack; s != nil {
			old := *s
			old.OldClient, old.OldToken, old.HumagenClient, old.HumagenToken = s.HumagenClient, s.HumagenToken, nil, nil
			p.Spec.Surfaces.Slack = &old
		}
		out.Platforms[i] = p
	}
	return marshal(out, legacy.APIVersion)
}

func marshal(cfg Config, version string) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	write := func(kind string, md Metadata, spec any) error {
		doc := []kv{{"api_version", version}, {"kind", kind}, {"metadata", md}, {"spec", spec}}
		node, err := toNode(doc)
		if err != nil {
			return err
		}
		return enc.Encode(node)
	}
	if err := write(KindSystem, cfg.System.Metadata, cfg.System.Spec); err != nil {
		return nil, err
	}
	for _, p := range cfg.Platforms {
		if err := write(KindPlatform, p.Metadata, p.Spec); err != nil {
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type kv struct {
	k string
	v any
}

// toNode turns a value into a YAML node, keeping a struct's field order (by
// way of its JSON) and dropping empty values.
func toNode(v any) (*yaml.Node, error) {
	if doc, ok := v.([]kv); ok {
		n := &yaml.Node{Kind: yaml.MappingNode}
		for _, e := range doc {
			child, err := toNode(e.v)
			if err != nil {
				return nil, err
			}
			if child == nil {
				continue
			}
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: e.k}, child)
		}
		return n, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return jsonToNode(json.NewDecoder(bytes.NewReader(b)))
}

// jsonToNode reads one JSON value from dec as an ordered YAML node. Empty
// objects, arrays, strings and nulls come back nil, so they are left out.
func jsonToNode(dec *json.Decoder) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			n := &yaml.Node{Kind: yaml.MappingNode}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				child, err := jsonToNode(dec)
				if err != nil {
					return nil, err
				}
				if child != nil {
					n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: kt.(string)}, child)
				}
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			if len(n.Content) == 0 {
				return nil, nil
			}
			return n, nil
		case '[':
			n := &yaml.Node{Kind: yaml.SequenceNode}
			for dec.More() {
				child, err := jsonToNode(dec)
				if err != nil {
					return nil, err
				}
				if child == nil {
					child = &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
				}
				n.Content = append(n.Content, child)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			if len(n.Content) == 0 {
				return nil, nil
			}
			return n, nil
		}
	case string:
		if t == "" {
			return nil, nil
		}
		n := &yaml.Node{Kind: yaml.ScalarNode, Value: t}
		// A string YAML would read as something else is quoted.
		var probe any
		if err := yaml.Unmarshal([]byte(t), &probe); err != nil || fmt.Sprint(probe) != t || strings.ContainsAny(t, ":#{}[],&*!|>'\"%@`") {
			n.Style = yaml.DoubleQuotedStyle
		}
		return n, nil
	case nil:
		return nil, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(t)}, nil
	case float64:
		tag := "!!float"
		if t == float64(int64(t)) {
			tag = "!!int"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: fmt.Sprint(t)}, nil
	}
	return nil, fmt.Errorf("spec: unexpected token %v", tok)
}
