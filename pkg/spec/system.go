package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/urmzd/humagen/fiber/pkg/bus"
	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/credentials"
)

// Slot names, for a binary registering a kind.
const (
	SlotAuth       = "auth"
	SlotStores     = "stores"
	SlotBus        = "bus"
	SlotBlobs      = "blobs"
	SlotExtensions = "extensions"
	SlotDeploy     = "deploy"
	SlotModels     = "models"
	SlotTools      = "tools"
)

// Builtin is the kinds each slot has with no extension. internal/app builds
// every one of them.
var Builtin = map[string][]string{
	SlotAuth:       {"dev", "tailscale", "oidc", "mock"},
	SlotStores:     {"memory", "postgres"},
	SlotBus:        {"memory", "postgres"},
	SlotBlobs:      {"disk", "memory", "s3"},
	SlotExtensions: {},
	SlotDeploy:     {"local", "compose", "kubernetes"},
	SlotModels:     {"anthropic", "openai", "ollama"},
	SlotTools:      {"mcp"},
}

// System is how and where humagen runs.
type System struct {
	Metadata Metadata   `json:"metadata"`
	Spec     SystemSpec `json:"spec"`
}

// SystemSpec is a System document's spec.
type SystemSpec struct {
	Notifications NotificationSettings `json:"notifications,omitzero"`
	Server        Server               `json:"server,omitzero"`
	Auth          Auth                 `json:"auth,omitzero"`
	Stores        Slot                 `json:"stores,omitzero"`
	Bus           Slot                 `json:"bus,omitzero"`
	Blobs         Slot                 `json:"blobs,omitzero"`
	Execution     Execution            `json:"execution,omitzero"`
	SecretEnv     map[string][]string  `json:"secret_env,omitempty"`
	// PersonRefs is where each workspace's people may keep their own tool
	// credentials, by tenant: how the references they give may start, each
	// naming the person whose references it holds with {subject}
	// (op://People/{subject}/, keychain:humagen-people-{subject}/,
	// env:HUMAGEN_PERSON_{subject}_). The server reads a reference with its
	// own access, so a person may give only their own: one under a start
	// with their subject in it. No two starts may share a place, within a
	// workspace or across two. A tenant not listed may reference nothing.
	PersonRefs map[string][]string `json:"person_refs,omitempty"`
	Extensions []Slot              `json:"extensions,omitempty"`
	// Deploy is where `humagen deploy` runs this: local, compose or
	// kubernetes. It is read by humagen deploy, not by the server.
	Deploy Slot `json:"deploy,omitzero"`
}

// Compose is the compose deploy kind's block: the platform as a Docker
// Compose project on this machine.
type Compose struct {
	// Dir is where the project is written. Empty is deploy/compose under
	// the spec's directory.
	Dir string `json:"dir,omitempty"`
	// Image is the humagen image. Empty builds one from Build.
	Image string `json:"image,omitempty"`
	// Build is a humagen checkout to build the image from. Empty is the
	// working directory when Image is empty.
	Build string `json:"build,omitempty"`
	// Port is the host port. Empty is the port of server.addr.
	Port int `json:"port,omitempty"`
	// Postgres runs a Postgres beside humagen and points the stores at it.
	Postgres bool `json:"postgres,omitempty"`
}

// Kubernetes is the kubernetes deploy kind's block: the platform as a Helm
// release of the chart humagen carries.
type Kubernetes struct {
	Context   string `json:"context,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	// Release is the Helm release name. Empty is metadata.name.
	Release string `json:"release,omitempty"`
	Image   struct {
		Repository string `json:"repository,omitempty"`
		Tag        string `json:"tag,omitempty"`
	} `json:"image,omitzero"`
	// Host is the ingress host. Empty means no ingress.
	Host         string `json:"host,omitempty"`
	IngressClass string `json:"ingress_class,omitempty"`
	TLSSecret    string `json:"tls_secret,omitempty"`
	// Values are further chart values, merged over what humagen derives.
	Values map[string]any `json:"values,omitempty"`
}

// Server is the listeners and the console.
type Server struct {
	Addr        string  `json:"addr,omitempty"`
	PublicURL   string  `json:"public_url,omitempty"`
	WebhookAddr string  `json:"webhook_addr,omitempty"`
	Console     Console `json:"console,omitzero"`
}

// Console is the web console the server serves.
type Console struct {
	// Enabled nil means on.
	Enabled *bool `json:"enabled,omitempty"`
	// Dir is a built console. Empty or "auto" finds one.
	Dir string `json:"dir,omitempty"`
}

// On reports whether the console is served.
func (c Console) On() bool { return c.Enabled == nil || *c.Enabled }

// Auth is who may call, as a slot (dev, tailscale, oidc, mock) with the
// fields every kind shares.
type Auth struct {
	Slot
	AuthShared
}

// AuthShared is what every sign-in kind shares.
type AuthShared struct {
	Tenant     Tenancy `json:"tenant,omitzero"`
	Groups     Groups  `json:"groups,omitzero"`
	PolicyFile string  `json:"policy_file,omitempty"`
}

// UnmarshalJSON reads the slot and the shared fields.
func (a *Auth) UnmarshalJSON(b []byte) error { return unmarshalSlot(b, &a.Slot, &a.AuthShared) }

// MarshalJSON writes them back.
func (a Auth) MarshalJSON() ([]byte, error) { return marshalSlot(a.Slot, a.AuthShared) }

// Tenancy is which workspace a caller is in: one fixed for every caller, or
// read from a token claim.
type Tenancy struct {
	Fixed string `json:"fixed,omitempty"`
	Claim string `json:"claim,omitempty"`
}

// Groups maps a token's groups claim to roles.
type Groups struct {
	Claim string              `json:"claim,omitempty"`
	Roles map[string][]string `json:"roles,omitempty"`
}

// OIDC is the oidc kind's block.
type OIDC struct {
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret *Ref   `json:"client_secret,omitempty"`
	Audience     string `json:"audience"`
}

// Mock is the mock kind's block: a loopback development identity provider.
type Mock struct {
	Addr     string `json:"addr,omitempty"`
	Personas string `json:"personas,omitempty"`
}

// URLBlock is a backend reached at a URL kept as a secret: the postgres
// store.
type URLBlock struct {
	URL *Ref `json:"url"`
}

// Disk is the disk blob kind's block.
type Disk struct {
	Dir string `json:"dir,omitempty"`
}

// S3 is the s3 blob kind's block.
type S3 struct {
	Bucket    string `json:"bucket"`
	Region    string `json:"region,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	PathStyle bool   `json:"path_style,omitempty"`
	AccessKey *Ref   `json:"access_key,omitempty"`
	SecretKey *Ref   `json:"secret_key,omitempty"`
}

// Execution bounds what runs.
type Execution struct {
	Workspace   string       `json:"workspace,omitempty"`
	Concurrency int          `json:"concurrency,omitempty"`
	MaxDepth    int          `json:"max_depth,omitempty"`
	MaxFanOut   int          `json:"max_fan_out,omitempty"`
	Builtin     BuiltinModel `json:"builtin,omitzero"`
}

// BuiltinModel is the model the platform's own agents run on.
type BuiltinModel struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

var (
	identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	envVar     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Defaults fills every empty field with what humagen uses for it.
func (s *System) Defaults() {
	if s.Metadata.Name == "" {
		s.Metadata.Name = legacy.DeployName
	}
	if s.Metadata.DisplayName == "" {
		s.Metadata.DisplayName = "Humagen"
	}
	sp := &s.Spec
	if sp.Server.Addr == "" {
		sp.Server.Addr = "127.0.0.1:8080"
	}
	if sp.Server.PublicURL == "" {
		sp.Server.PublicURL = "http://localhost:8080"
	}
	if sp.Auth.Kind == "" {
		sp.Auth.Kind = "dev"
	}
	if sp.Auth.Tenant == (Tenancy{}) {
		sp.Auth.Tenant.Fixed = "default"
	}
	if sp.Auth.Groups.Claim == "" {
		sp.Auth.Groups.Claim = "groups"
	}
	if sp.Stores.Kind == "" {
		sp.Stores.Kind = "memory"
	}
	// Postgres first: stores on Postgres put the bus there too, so replicas
	// on one database share one stream with nothing else to run.
	if sp.Bus.Kind == "" {
		sp.Bus.Kind = "memory"
		if sp.Stores.Kind == "postgres" {
			sp.Bus.Kind = "postgres"
		}
	}
	if sp.Blobs.Kind == "" {
		sp.Blobs.Kind = "disk"
	}
	if sp.Execution.MaxDepth == 0 {
		sp.Execution.MaxDepth = 4
	}
	if sp.Execution.MaxFanOut == 0 {
		sp.Execution.MaxFanOut = 16
	}
	if sp.Execution.Concurrency == 0 {
		sp.Execution.Concurrency = 1
	}
}

// Tenant is the fixed tenant, or empty when it comes from a claim.
func (s System) Tenant() string { return s.Spec.Auth.Tenant.Fixed }

func (s System) check(kinds Kinds) error {
	sp := s.Spec
	if err := sp.Notifications.check(); err != nil {
		return err
	}
	if s.Metadata.Name != "" && !identifier.MatchString(s.Metadata.Name) {
		return fmt.Errorf("system: metadata.name %q: lowercase letters, digits and dashes", s.Metadata.Name)
	}
	if strings.ContainsAny(s.Metadata.DisplayName, "\r\n\t<>") || len(s.Metadata.DisplayName) > 80 {
		return fmt.Errorf("system: metadata.display_name is plain text on one line, at most 80 characters")
	}
	if t := sp.Auth.Tenant; t.Fixed != "" && t.Claim != "" {
		return fmt.Errorf("system: auth.tenant: fixed or claim, not both")
	}
	for name, vars := range sp.SecretEnv {
		for _, v := range vars {
			if !envVar.MatchString(v) || legacy.SignInSecret(v) {
				return fmt.Errorf("system: secret_env.%s: %q cannot be referenced by a client", name, v)
			}
		}
	}
	for name, starts := range sp.PersonRefs {
		for _, start := range starts {
			if err := credentials.CheckStart(start); err != nil {
				return fmt.Errorf("system: person_refs.%s: %w", name, err)
			}
		}
	}
	if err := credentials.CheckStarts(sp.PersonRefs); err != nil {
		return fmt.Errorf("system: person_refs: %w", err)
	}
	slots := []struct {
		name string
		slot Slot
	}{{SlotAuth, sp.Auth.Slot}, {SlotStores, sp.Stores}, {SlotBus, sp.Bus}, {SlotBlobs, sp.Blobs}, {SlotDeploy, sp.Deploy}}
	for _, e := range sp.Extensions {
		slots = append(slots, struct {
			name string
			slot Slot
		}{SlotExtensions, e})
	}
	// A child at depth d triggers with d hops and its run events carry one
	// more, so the deepest child must stay within the bus's hop limit.
	if d := sp.Execution.MaxDepth; d < 0 || d > bus.DefaultMaxHops-1 {
		return fmt.Errorf("system: execution.max_depth: %d is outside 1 to %d, the bus's hop limit less one", d, bus.DefaultMaxHops-1)
	}
	if sp.Bus.Kind == "redis" {
		return errors.New("system: bus: the redis bus was removed; use kind: postgres, which shares the stores' database, or leave bus out")
	}
	if sp.Bus.Kind == "postgres" && sp.Stores.Kind != "postgres" {
		return errors.New("system: bus: kind postgres shares the stores' database; set stores.kind: postgres")
	}
	for _, x := range slots {
		if err := kinds.check(x.name, x.slot); err != nil {
			return fmt.Errorf("system: %s: %w", x.name, err)
		}
	}
	return nil
}

// builtinBlock is the block type of each built-in kind, so a built-in is
// checked when the file is read.
func builtinBlock(slot, kind string) (any, bool) {
	switch slot + "." + kind {
	case "auth.dev", "auth.tailscale", "stores.memory", "bus.memory", "blobs.memory", "deploy.local":
		return &struct{}{}, true
	case "deploy.compose":
		return &Compose{}, true
	case "deploy.kubernetes":
		return &Kubernetes{}, true
	case "auth.oidc":
		return &OIDC{}, true
	case "auth.mock":
		return &Mock{}, true
	case "bus.postgres":
		// The bus shares the stores' database, so it has nothing of its own.
		return &struct{}{}, true
	case "stores.postgres":
		return &URLBlock{}, true
	case "blobs.disk":
		return &Disk{}, true
	case "blobs.s3":
		return &S3{}, true
	case "models.anthropic", "models.openai", "models.ollama":
		return &ModelBlock{}, true
	case "tools.mcp":
		return &MCP{}, true
	}
	return nil, false
}

// Kinds is which kinds a binary has beyond the built-ins, by slot.
type Kinds map[string][]string

func (k Kinds) check(slot string, s Slot) error {
	if s.Kind == "" {
		return nil
	}
	if blk, ok := builtinBlock(slot, s.Kind); ok {
		if err := s.Decode(blk); err != nil {
			return fmt.Errorf("%s: %w", s.Kind, err)
		}
		return checkBlock(slot+"."+s.Kind, blk)
	}
	if k == nil {
		return nil
	}
	for _, name := range k[slot] {
		if name == s.Kind {
			// A registered kind decodes its own block when it is built.
			return nil
		}
	}
	return fmt.Errorf("kind %q is not one this binary has (%s)", s.Kind, strings.Join(append(append([]string{}, Builtin[slot]...), k[slot]...), ", "))
}

// checkBlock enforces what a decoded built-in block needs.
func checkBlock(where string, blk any) error {
	switch b := blk.(type) {
	case *OIDC:
		if b.Issuer == "" || b.ClientID == "" || b.Audience == "" {
			return fmt.Errorf("%s: issuer, client_id and audience are required", where)
		}
	case *URLBlock:
		if !b.URL.Set() {
			return fmt.Errorf("%s: url is required, as {ref: env:NAME}", where)
		}
	case *S3:
		if b.Bucket == "" {
			return fmt.Errorf("%s: bucket is required", where)
		}
		if b.AccessKey.Set() != b.SecretKey.Set() {
			return fmt.Errorf("%s: access_key and secret_key go together", where)
		}
	}
	return nil
}

// Block decodes a built-in kind's block. It is for internal/app, after Check.
func Block[T any](s Slot) (T, error) {
	var v T
	err := s.Decode(&v)
	return v, err
}

// MustJSON is json.Marshal for values that cannot fail.
func MustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
