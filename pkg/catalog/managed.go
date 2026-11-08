package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/urmzd/humagen/pkg/state"
)

func definitionKey(name string) string { return "d/" + name }

// CreateManaged atomically reserves the connector name, private definition
// and quota slot. Its caller validates the kind-specific definition. Published
// metadata and successful crawl state cannot be supplied during creation.
func (s *Store) CreateManaged(tenant string, c Connector, definition json.RawMessage) (Connector, error) {
	if tenant == "" || !ValidName(c.Name) || c.System == "" || c.Owner == "" || len(definition) == 0 || len(definition) > 16384 || !json.Valid(definition) {
		return Connector{}, fmt.Errorf("%w: managed connector definition is invalid", ErrInvalid)
	}
	if err := s.dir.Mark(tenant); err != nil {
		return Connector{}, err
	}
	next := Connector{Name: c.Name, System: c.System, Owner: c.Owner, Project: c.Project}
	err := state.Retry(func() error {
		if _, _, err := s.connector(tenant, c.Name); err == nil {
			return ErrExists
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if _, err := s.kept.Lookup(tenant, definitionKey(c.Name)); err == nil {
			return ErrExists
		} else if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		have, head, err := s.connectors(tenant)
		if err != nil {
			return err
		}
		if len(have) >= MaxConnectors {
			return fmt.Errorf("%w: at most %d connectors", ErrInvalid, MaxConnectors)
		}
		b := s.kept.Batch()
		b.Create(tenant, connectorKey(c.Name), record{Connector: &next})
		b.Create(tenant, definitionKey(c.Name), record{Definition: bytes.Clone(definition)})
		bump(b, tenant, head)
		return b.Commit()
	})
	if err != nil {
		return Connector{}, err
	}
	return next, nil
}

// ManagedDefinition is runtime-only configuration. Catalog responses never
// include it. It is immutable; replacing a saved connection is a separate
// lifecycle operation rather than an unversioned overwrite.
func (s *Store) ManagedDefinition(tenant, name string) (json.RawMessage, error) {
	it, err := s.kept.Lookup(tenant, definitionKey(name))
	if errors.Is(err, state.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(it.Value.Definition) == 0 || !json.Valid(it.Value.Definition) {
		return nil, fmt.Errorf("%w: persisted connector definition is invalid", ErrInvalid)
	}
	return bytes.Clone(it.Value.Definition), nil
}
