package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/urmzd/humagen/pkg/catalog"
)

// An ontology is the types a corpus adds to the workspace graph, written as
// a JSON Schema (2020-12): each entity type is an object schema under
// $defs, with its properties and required fields. Three extensions say
// what plain JSON Schema cannot:
//
//	x-relations      relation types: a name, the types it joins, cardinality
//	x-metadata       passage metadata fields search can filter on
//	$defs.T.x-matches        names of tables or document titles of type T
//	$defs.T.x-half-life-days how fast a document of type T ages
//
// A corpus extends the core types (person, team, project, system, topic,
// document) and never redefines them.

// Bounds on one ontology.
const (
	MaxOntologyBytes = 64 << 10
	MaxTypes         = 100
	MaxProperties    = 100
	MaxRelations     = 200
	MaxMetadata      = 50
)

var (
	typeName     = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{0,62}$`)
	propertyName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	jsonTypes    = []string{"string", "number", "integer", "boolean", "array", "object"}
	coreTypes    = []string{Person, Team, Project, System, Topic, Document}
	// integerNumber is a NUMBER or DECIMAL with no scale, as Snowflake
	// writes whole numbers.
	integerNumber = regexp.MustCompile(`^(NUMBER|NUMERIC|DECIMAL)\(\d+(,\s*0)?\)$`)
)

// Property is one property of a type, as a JSON Schema.
type Property struct {
	Type        string `json:"type"`
	Format      string `json:"format,omitempty"`
	Description string `json:"description,omitempty"`
}

// Type is one entity type: an object schema.
type Type struct {
	Type        string              `json:"type"`
	Description string              `json:"description,omitempty"`
	Properties  map[string]Property `json:"properties,omitempty"`
	Required    []string            `json:"required,omitempty"`
	Matches     []string            `json:"x-matches,omitempty"`
	HalfLife    int                 `json:"x-half-life-days,omitempty"`
}

// Relation is one relation type between two types.
type Relation struct {
	Name        string `json:"name"`
	From        string `json:"from"`
	To          string `json:"to"`
	Cardinality string `json:"cardinality,omitempty"`
}

// Ontology is a corpus's types.
type Ontology struct {
	Schema      string              `json:"$schema,omitempty"`
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Types       map[string]Type     `json:"$defs,omitempty"`
	Relations   []Relation          `json:"x-relations,omitempty"`
	Metadata    map[string]Property `json:"x-metadata,omitempty"`
}

// SchemaDraft is the JSON Schema an ontology is written in.
const SchemaDraft = "https://json-schema.org/draft/2020-12/schema"

func known(o Ontology, name string) bool {
	_, ok := o.Types[name]
	return ok || slices.Contains(coreTypes, name)
}

func checkProperty(where string, p Property) error {
	if !slices.Contains(jsonTypes, p.Type) || len(p.Format) > 32 || len(p.Description) > 500 {
		return fmt.Errorf("%w: %s: a property's type is one of %s", ErrInvalid, where, strings.Join(jsonTypes, ", "))
	}
	return nil
}

// ParseOntology reads and checks an ontology. Unknown keywords are refused,
// so a typo is an error, not a silently ignored rule.
func ParseOntology(raw json.RawMessage) (Ontology, error) {
	var o Ontology
	if len(raw) > MaxOntologyBytes {
		return o, fmt.Errorf("%w: an ontology is at most %d bytes", ErrInvalid, MaxOntologyBytes)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&o); err != nil {
		return o, fmt.Errorf("%w: an ontology is a JSON Schema with $defs, x-relations and x-metadata: %v", ErrInvalid, err)
	}
	if d.More() {
		return o, fmt.Errorf("%w: an ontology is one JSON object", ErrInvalid)
	}
	if o.Schema != "" && o.Schema != SchemaDraft {
		return o, fmt.Errorf("%w: $schema is %s", ErrInvalid, SchemaDraft)
	}
	if len(o.Types) > MaxTypes || len(o.Relations) > MaxRelations || len(o.Metadata) > MaxMetadata {
		return o, fmt.Errorf("%w: at most %d types, %d relations and %d metadata fields", ErrInvalid, MaxTypes, MaxRelations, MaxMetadata)
	}
	for name, t := range o.Types {
		if !typeName.MatchString(name) || slices.Contains(coreTypes, strings.ToLower(name)) {
			return o, fmt.Errorf("%w: type %q is PascalCase and not a core type", ErrInvalid, name)
		}
		if t.Type != "object" || len(t.Properties) > MaxProperties || len(t.Matches) > 20 || t.HalfLife < 0 || t.HalfLife > 3650 || len(t.Description) > 500 {
			return o, fmt.Errorf("%w: type %q is an object of at most %d properties, 20 matches and a half-life of at most 3650 days", ErrInvalid, name, MaxProperties)
		}
		for prop, p := range t.Properties {
			if !propertyName.MatchString(prop) {
				return o, fmt.Errorf("%w: %s.%s is snake_case", ErrInvalid, name, prop)
			}
			if err := checkProperty(name+"."+prop, p); err != nil {
				return o, err
			}
		}
		for _, r := range t.Required {
			if _, ok := t.Properties[r]; !ok {
				return o, fmt.Errorf("%w: %s requires %q, which it does not define", ErrInvalid, name, r)
			}
		}
		for _, m := range t.Matches {
			if m == "" || len(m) > 64 {
				return o, fmt.Errorf("%w: %s's matches are 1 to 64 bytes", ErrInvalid, name)
			}
		}
	}
	for _, r := range o.Relations {
		if !propertyName.MatchString(r.Name) || !known(o, r.From) || !known(o, r.To) || (r.Cardinality != "" && r.Cardinality != "one" && r.Cardinality != "many") {
			return o, fmt.Errorf("%w: relation %q joins two known types with cardinality one or many", ErrInvalid, r.Name)
		}
	}
	for field, p := range o.Metadata {
		if !propertyName.MatchString(field) {
			return o, fmt.Errorf("%w: metadata field %q is snake_case", ErrInvalid, field)
		}
		if err := checkProperty("x-metadata."+field, p); err != nil {
			return o, err
		}
	}
	return o, nil
}

// Raw writes an ontology back as its canonical JSON.
func (o Ontology) Raw() json.RawMessage {
	if o.Schema == "" {
		o.Schema = SchemaDraft
	}
	raw, _ := json.Marshal(o)
	return raw
}

// TypeOf is the type whose matches, or whose name in the singular or the
// plural, a table or document name holds, and "" when none does.
func (o Ontology) TypeOf(name string) string {
	key := strings.ToLower(lastSegment(name))
	names := make([]string, 0, len(o.Types))
	for n := range o.Types {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t := o.Types[n]
		lower := strings.ToLower(n)
		if key == lower || key == plural(lower) || key == snake(n) || key == plural(snake(n)) {
			return n
		}
		for _, m := range t.Matches {
			if strings.EqualFold(m, key) || strings.Contains(strings.ToLower(name), strings.ToLower(m)) {
				return n
			}
		}
	}
	return ""
}

func lastSegment(path string) string {
	if i := strings.LastIndexAny(path, "./"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func plural(s string) string {
	switch {
	case strings.HasSuffix(s, "y") && len(s) > 1 && !strings.ContainsRune("aeiou", rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	}
	return s + "s"
}

func singular(s string) string {
	switch {
	case s == "people":
		return "person"
	case strings.HasSuffix(s, "ies") && len(s) > 3:
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "ses"), strings.HasSuffix(s, "xes"), strings.HasSuffix(s, "ches"), strings.HasSuffix(s, "shes"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss"):
		return s[:len(s)-1]
	}
	return s
}

// snake is a PascalCase name in snake_case.
func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// pascal is a table name as a type name: customer_orders is CustomerOrder.
func pascal(s string) string {
	parts := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(parts) == 0 {
		return ""
	}
	parts[len(parts)-1] = singular(parts[len(parts)-1])
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	out := b.String()
	if !typeName.MatchString(out) {
		return ""
	}
	return out
}

func propertyType(column string) Property {
	t := strings.ToUpper(column)
	switch {
	case strings.Contains(t, "BOOL"):
		return Property{Type: "boolean"}
	case strings.Contains(t, "INT"), t == "NUMBER", integerNumber.MatchString(t):
		return Property{Type: "integer"}
	case strings.Contains(t, "FLOAT"), strings.Contains(t, "DOUBLE"), strings.Contains(t, "NUMERIC"), strings.Contains(t, "DECIMAL"), strings.Contains(t, "NUMBER"), strings.Contains(t, "REAL"):
		return Property{Type: "number"}
	case strings.Contains(t, "TIMESTAMP"), strings.Contains(t, "DATETIME"):
		return Property{Type: "string", Format: "date-time"}
	case t == "DATE":
		return Property{Type: "string", Format: "date"}
	case strings.Contains(t, "ARRAY"), t == "REPEATED":
		return Property{Type: "array"}
	case strings.Contains(t, "RECORD"), strings.Contains(t, "STRUCT"), strings.Contains(t, "OBJECT"), strings.Contains(t, "VARIANT"), strings.Contains(t, "JSON"):
		return Property{Type: "object"}
	}
	return Property{Type: "string"}
}

// Infer proposes an ontology from warehouse tables, with no model calls:
// a type per table, its columns as properties, and a relation for each
// column that names another table's type with _id. It is a proposal: an
// owner accepts what they want.
func Infer(entries []catalog.Entry) Ontology {
	o := Ontology{Schema: SchemaDraft, Types: map[string]Type{}}
	byType := map[string]catalog.Entry{}
	for _, e := range entries {
		if len(o.Types) >= MaxTypes {
			break
		}
		name := pascal(lastSegment(e.Path))
		if name == "" || slices.Contains(coreTypes, strings.ToLower(name)) {
			continue
		}
		if _, dup := o.Types[name]; dup {
			continue
		}
		t := Type{Type: "object", Description: clipped(e.Description, 500), Properties: map[string]Property{}, Matches: []string{strings.ToLower(lastSegment(e.Path))}}
		for _, c := range e.Schema {
			prop := strings.ToLower(c.Name)
			if !propertyName.MatchString(prop) || len(t.Properties) >= MaxProperties {
				continue
			}
			p := propertyType(c.Type)
			if c.Mode == "REPEATED" {
				p = Property{Type: "array"}
			}
			p.Description = clipped(c.Description, 500)
			t.Properties[prop] = p
		}
		o.Types[name] = t
		byType[name] = e
	}
	names := make([]string, 0, len(o.Types))
	for n := range o.Types {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, from := range names {
		props := make([]string, 0, len(o.Types[from].Properties))
		for p := range o.Types[from].Properties {
			props = append(props, p)
		}
		sort.Strings(props)
		for _, p := range props {
			ref, ok := strings.CutSuffix(p, "_id")
			if !ok || ref == "" {
				continue
			}
			to := pascal(ref)
			if _, exists := o.Types[to]; !exists || to == from || len(o.Relations) >= MaxRelations {
				continue
			}
			o.Relations = append(o.Relations, Relation{Name: ref, From: from, To: to, Cardinality: "one"})
		}
	}
	return o
}

func clipped(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

// Accept adds proposed types to declared ones: a type declared already
// keeps its declaration, and a relation is added once. only, when not
// empty, accepts just those types and the relations between accepted types.
func Accept(declared, proposed Ontology, only []string) Ontology {
	out := declared
	out.Schema = SchemaDraft
	out.Types = map[string]Type{}
	for n, t := range declared.Types {
		out.Types[n] = t
	}
	for n, t := range proposed.Types {
		if _, have := out.Types[n]; have || (len(only) > 0 && !slices.Contains(only, n)) {
			continue
		}
		out.Types[n] = t
	}
	out.Relations = slices.Clone(declared.Relations)
	for _, r := range proposed.Relations {
		if !known(out, r.From) || !known(out, r.To) {
			continue
		}
		if !slices.ContainsFunc(out.Relations, func(h Relation) bool { return h.Name == r.Name && h.From == r.From && h.To == r.To }) {
			out.Relations = append(out.Relations, r)
		}
	}
	return out
}
