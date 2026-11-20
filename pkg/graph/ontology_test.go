package graph

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/urmzd/humagen/pkg/catalog"
)

const hrOntology = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$defs": {
    "Policy": {"type": "object", "properties": {"jurisdiction": {"type": "string"}, "effective_date": {"type": "string", "format": "date"}}, "required": ["jurisdiction"], "x-matches": ["policy"], "x-half-life-days": 730},
    "Employee": {"type": "object", "properties": {"start_date": {"type": "string", "format": "date"}}}
  },
  "x-relations": [{"name": "governs", "from": "Policy", "to": "Employee", "cardinality": "many"}, {"name": "managed_by", "from": "Employee", "to": "person"}],
  "x-metadata": {"jurisdiction": {"type": "string"}}
}`

func TestADeclaredOntologyIsChecked(t *testing.T) {
	o, err := ParseOntology(json.RawMessage(hrOntology))
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Types) != 2 || len(o.Relations) != 2 || o.Types["Policy"].HalfLife != 730 {
		t.Fatalf("parsed %+v", o)
	}
	if again, err := ParseOntology(o.Raw()); err != nil || len(again.Types) != 2 {
		t.Fatalf("its canonical form does not read back: %v", err)
	}
	for name, raw := range map[string]string{
		"a core type redefined":    `{"$defs": {"Person": {"type": "object"}}}`,
		"a lower case type":        `{"$defs": {"policy": {"type": "object"}}}`,
		"a type that is no object": `{"$defs": {"Policy": {"type": "string"}}}`,
		"an unknown property type": `{"$defs": {"Policy": {"type": "object", "properties": {"x": {"type": "date"}}}}}`,
		"a required undefined":     `{"$defs": {"Policy": {"type": "object", "required": ["x"]}}}`,
		"a relation to nothing":    `{"x-relations": [{"name": "governs", "from": "Policy", "to": "Employee"}]}`,
		"an unknown keyword":       `{"$defs": {}, "x-rules": []}`,
		"another draft":            `{"$schema": "http://json-schema.org/draft-07/schema#"}`,
		"not an object":            `[]`,
	} {
		if _, err := ParseOntology(json.RawMessage(raw)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAnOntologyIsInferredFromWarehouseTables(t *testing.T) {
	o := Infer([]catalog.Entry{
		{Path: "acme.sales.customers", Description: "People who buy", Schema: []catalog.Column{{Name: "id", Type: "INT64"}, {Name: "name", Type: "STRING"}, {Name: "signed_up", Type: "TIMESTAMP"}}},
		{Path: "acme.sales.orders", Schema: []catalog.Column{{Name: "id", Type: "NUMBER(38,0)"}, {Name: "customer_id", Type: "INT64"}, {Name: "total", Type: "NUMERIC"}, {Name: "tags", Type: "STRING", Mode: "REPEATED"}}},
		{Path: "acme.sales.people", Schema: []catalog.Column{{Name: "id", Type: "STRING"}}},
	})
	if _, err := ParseOntology(o.Raw()); err != nil {
		t.Fatalf("an inferred ontology does not check: %v", err)
	}
	customer, order := o.Types["Customer"], o.Types["Order"]
	if customer.Description != "People who buy" || customer.Properties["signed_up"].Format != "date-time" || order.Properties["total"].Type != "number" || order.Properties["id"].Type != "integer" || order.Properties["tags"].Type != "array" {
		t.Fatalf("types %+v", o.Types)
	}
	if _, core := o.Types["Person"]; core {
		t.Fatal("a table named for a core type became a type")
	}
	if !slices.Equal(o.Relations, []Relation{{Name: "customer", From: "Order", To: "Customer", Cardinality: "one"}}) {
		t.Fatalf("relations %+v", o.Relations)
	}
	if o.TypeOf("ACME.SALES.ORDERS") != "Order" || o.TypeOf("acme.sales.customer") != "Customer" || o.TypeOf("acme.sales.invoices") != "" {
		t.Fatal("tables are not typed by name")
	}
}

func TestAcceptingAProposalKeepsWhatIsDeclared(t *testing.T) {
	declared, _ := ParseOntology(json.RawMessage(hrOntology))
	proposed := Ontology{Types: map[string]Type{"Policy": {Type: "object", Description: "inferred"}, "Contract": {Type: "object"}, "Invoice": {Type: "object"}}, Relations: []Relation{{Name: "about", From: "Contract", To: "Employee"}, {Name: "billed", From: "Invoice", To: "Contract"}}}
	got := Accept(declared, proposed, []string{"Contract", "Policy"})
	if got.Types["Policy"].Description == "inferred" || len(got.Types) != 3 {
		t.Fatalf("accepted types %+v", got.Types)
	}
	if len(got.Relations) != 3 || slices.ContainsFunc(got.Relations, func(r Relation) bool { return r.Name == "billed" }) {
		t.Fatalf("accepted relations %+v", got.Relations)
	}
	if _, err := ParseOntology(got.Raw()); err != nil {
		t.Fatalf("an accepted ontology does not check: %v", err)
	}
}
