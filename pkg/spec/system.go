package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/bus"
	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/rooms"
)

// Slot names, for a binary registering a kind.
const (
	SlotAuth       = "auth"
	SlotStores     = "stores"
	SlotBus        = "bus"
	SlotBlobs      = "blobs"
	SlotEmbedder   = "embedder"
	SlotExtensions = "extensions"
	SlotDeploy     = "deploy"
	SlotSandbox    = "sandbox"
	SlotEgress     = "egress"
	SlotModels     = "models"
	SlotTools      = "tools"
	SlotConnectors = "connectors"
)

// Builtin is the kinds each slot has with no extension. internal/app builds
// every one of them.
var Builtin = map[string][]string{
	SlotAuth:       {"dev", "tailscale", "oidc", "mock"},
	SlotStores:     {"memory", "postgres"},
	SlotBus:        {"memory", "postgres"},
	SlotBlobs:      {"disk", "gcs", "memory", "s3"},
	SlotEmbedder:   {"none", "hashing", "openai", "vertex"},
	SlotExtensions: {},
	SlotDeploy:     {"local", "compose", "kubernetes"},
	SlotSandbox:    {"docker", "local", "unisolated"},
	SlotEgress:     {"none", "gate"},
	SlotModels:     {"anthropic", "openai", "google"},
	SlotTools:      {"mcp"},
	SlotConnectors: {"bigquery"},
}

// System is how and where humagen runs.
type System struct {
	Metadata Metadata   `json:"metadata"`
	Spec     SystemSpec `json:"spec"`
}

// SystemSpec is a System document's spec.
type SystemSpec struct {
	// Branding is the deployment's look and name in the console.
	Branding      Branding             `json:"branding,omitzero"`
	Notifications NotificationSettings `json:"notifications,omitzero"`
	Server        Server               `json:"server,omitzero"`
	Auth          Auth                 `json:"auth,omitzero"`
	Stores        Slot                 `json:"stores,omitzero"`
	Bus           Slot                 `json:"bus,omitzero"`
	Blobs         Slot                 `json:"blobs,omitzero"`
	// Knowledge is how knowledge sources are searched (RFC 0005).
	Knowledge Knowledge `json:"knowledge,omitzero"`
	Execution Execution `json:"execution,omitzero"`
	// Sandbox is where a run's tools execute: the file tools, the shell
	// and stdio MCP servers, in an instance of the run's own. docker (the
	// default, with the runsc runtime) isolates them; local and
	// unisolated run them on this host and are for development only.
	Sandbox Slot `json:"sandbox,omitzero"`
	// Egress is how a sandbox instance reaches the network: none (the
	// default, no network at all) or gate, playscape's egress proxy,
	// through which a run reaches only the hosts its egress ceiling
	// allows.
	Egress    Slot                `json:"egress,omitzero"`
	SecretEnv map[string][]string `json:"secret_env,omitempty"`
	// Rooms is how a conversation with many readers is read, and how fast
	// runs start in it (RFC 0004).
	Rooms Rooms `json:"rooms,omitzero"`
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
	// DrainGrace is how long, after SIGTERM, runs in flight may finish
	// before what is left is handed back for another process to resume, a
	// Go duration. Left out, 25s.
	DrainGrace string `json:"drain_grace,omitempty"`
	// ShutdownTimeout is how long open requests, event streams among them,
	// may take to end once the drain is over, a Go duration. Left out, 10s.
	ShutdownTimeout string `json:"shutdown_timeout,omitempty"`
}

// Defaults for Server's durations.
const (
	DefaultDrainGrace      = 25 * time.Second
	DefaultShutdownTimeout = 10 * time.Second
)

// check refuses a duration that is not one, or is negative.
func (s Server) check() error {
	for _, f := range []struct{ name, v string }{{"drain_grace", s.DrainGrace}, {"shutdown_timeout", s.ShutdownTimeout}} {
		if f.v == "" {
			continue
		}
		if d, err := time.ParseDuration(f.v); err != nil || d < 0 {
			return fmt.Errorf("server: %s %q is not a duration, such as 25s", f.name, f.v)
		}
	}
	return nil
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

// EgressGate is the gate egress kind's block: an HTTP forward proxy this
// server runs, and every sandbox instance's only way out. A run reaches
// the hosts its egress ceiling allows (workspace.run.ceilings.egress, then
// its projects and person), and no other; every decision is audited.
type EgressGate struct {
	// Listen is where the gate listens, host:port. Empty is
	// DefaultEgressListen, which only a sandbox on this host (local) can
	// reach.
	Listen string `json:"listen,omitempty"`
	// Address is the gate as an instance reaches it, host:port. Under
	// docker it is an address on Network; give an IP address, since
	// gVisor cannot always resolve names there. Empty is Listen.
	Address string `json:"address,omitempty"`
	// Network is the Docker network docker instances join: created with
	// --internal, so it has no route out but to the gate. The docker
	// sandbox refuses egress without it.
	Network string `json:"network,omitempty"`
	// AllowNonPublic lets the gate dial loopback, private and link-local
	// addresses an allowed host resolves to. Off, they are refused, so a
	// host name cannot lead a run into the private network.
	AllowNonPublic bool `json:"allow_non_public,omitempty"`
}

// DefaultEgressListen is where the gate listens when the spec says nothing.
const DefaultEgressListen = "127.0.0.1:3128"

// Disk is the disk blob kind's block.
type Disk struct {
	Dir string `json:"dir,omitempty"`
}

// GCS is the gcs blob kind's block: a Google Cloud Storage bucket, reached
// with Application Default Credentials (Workload Identity on GKE,
// GOOGLE_APPLICATION_CREDENTIALS, or gcloud's application default login).
type GCS struct {
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix,omitempty"`
	// Endpoint is an emulator's base URL, such as http://127.0.0.1:4443.
	// Empty means Google Cloud Storage itself.
	Endpoint string `json:"endpoint,omitempty"`
}

// DockerSandbox is the docker sandbox kind's block. Each run's tools
// execute in containers of their own: no network, a read-only root, no
// capabilities, a non-root user, and CPU, memory, process and disk limits.
type DockerSandbox struct {
	// Runtime is the OCI runtime: runsc (gVisor, the default) or runc,
	// which shares the host's kernel and is for trusted tools.
	Runtime string `json:"runtime,omitempty"`
	// InsecureDevelopmentRuntime accepts a runtime that shares the host's
	// kernel, such as runc. Without it such a runtime is refused and every
	// tool that needs an instance says why. Set it only for development
	// machines without gVisor, where the code that runs is your own.
	InsecureDevelopmentRuntime bool `json:"insecure_development_runtime,omitempty"`
	// Image is the workload's image: it must have sh and sleep. Empty is
	// DefaultSandboxImage.
	Image string `json:"image,omitempty"`
	// ConnectorsImage is where stdio MCP servers run, which must have
	// their commands. Empty is Image.
	ConnectorsImage string `json:"connectors_image,omitempty"`
	// WorkRoot is the host directory the runs' work directories are made
	// in, which the Docker daemon must be able to mount. Empty is
	// humagen/sandbox in the user cache directory.
	WorkRoot string `json:"work_root,omitempty"`
	// Host is a Docker daemon on another machine, a sandbox host, as
	// tcp://, ssh:// or unix://. Humagen then holds no Docker socket on
	// its own machine, and work_root is unused: a run's work directory is
	// a volume on the sandbox host. Empty is the local daemon.
	Host string `json:"host,omitempty"`
	// TLSDir holds ca.pem, cert.pem and key.pem for host: the daemon's
	// certificate authority and Humagen's client certificate. A tcp://
	// host requires it.
	TLSDir string `json:"tls_dir,omitempty"`
	// Warm is how many instances are kept started ahead of runs, per
	// shape; Max bounds every instance at once. Zero is playscape's
	// default (1 and 64).
	Warm int `json:"warm,omitempty"`
	Max  int `json:"max,omitempty"`
}

// LocalSandbox is the local sandbox kind's block. Its instances are NOT
// isolated: a run's tools execute as host processes with the server's
// privileges, in a directory laid out like an instance. It gives the shell
// on a machine without Docker, and is for development only.
type LocalSandbox struct {
	// Dir is where instance directories are made. Empty is
	// humagen/sandbox in the user cache directory.
	Dir string `json:"dir,omitempty"`
}

var runtimeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// DefaultSandboxImage is the docker sandbox's image when the spec names
// none: a shell, the busybox tools and python3.
const DefaultSandboxImage = "python:3.13-alpine"

// Sandbox runtimes the docker kind knows by name.
const (
	RuntimeGVisor = "runsc"
	RuntimeRunc   = "runc"
)

// Knowledge is how knowledge sources are searched.
type Knowledge struct {
	// Embedder gives passages vectors, and search then fuses vector and
	// lexical rankings: none (the default, lexical only), hashing, openai or
	// vertex.
	Embedder Slot `json:"embedder,omitzero"`
}

// HashingEmbedder is the hashing embedder kind's block: deterministic, no
// model and no network. It matches shared words and word stems, not meaning.
type HashingEmbedder struct {
	Dimensions int `json:"dimensions,omitempty"`
}

// OpenAIEmbedder is the openai embedder kind's block: any server with an
// OpenAI-compatible /embeddings endpoint.
type OpenAIEmbedder struct {
	BaseURL    string `json:"base_url,omitempty"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
	Key        *Ref   `json:"key,omitempty"`
}

// VertexEmbedder is the vertex embedder kind's block: Vertex AI with
// Application Default Credentials. Model and dimensions default to
// gemini-embedding-001 at 768.
type VertexEmbedder struct {
	Project    string `json:"project"`
	Location   string `json:"location,omitempty"`
	Model      string `json:"model,omitempty"`
	Dimensions int    `json:"dimensions,omitempty"`
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

// Rooms is how a conversation with many readers is read, and how fast runs
// start in it (pkg/rooms). Each default is far above what an ordinary
// conversation reaches, so leaving it out changes nothing for one.
type Rooms struct {
	// CoalesceAbove is how many readers a conversation has on one server
	// before each is sent at once only what involves them, and the rest as
	// a page every TailInterval. Left out, 200.
	CoalesceAbove int `json:"coalesce_above,omitempty"`
	// TailInterval is how often a large conversation's page is sent, a Go
	// duration from 500ms to 1m. Left out, 3s.
	TailInterval string `json:"tail_interval,omitempty"`
	// MaxRunsPerMinute is how many runs one conversation starts in any
	// minute. Left out, 600.
	MaxRunsPerMinute int `json:"max_runs_per_minute,omitempty"`
	// MaxRunsPerPerson is how many runs one person has in flight at once.
	// Left out, 25.
	MaxRunsPerPerson int `json:"max_runs_per_person,omitempty"`
}

// Config is the rooms block as pkg/rooms reads it, its defaults filled.
func (r Rooms) Config() (rooms.Config, error) {
	c := rooms.Config{CoalesceAbove: r.CoalesceAbove, RoomRunsPerMinute: r.MaxRunsPerMinute, PersonRuns: r.MaxRunsPerPerson}
	if r.TailInterval != "" {
		d, err := time.ParseDuration(r.TailInterval)
		if err != nil {
			return c, fmt.Errorf("rooms: tail_interval %q is not a duration, such as 3s", r.TailInterval)
		}
		c.TailInterval = d
	}
	if err := c.Check(); err != nil {
		return c, err
	}
	return c.Defaults(), nil
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
	if sp.Knowledge.Embedder.Kind == "" {
		sp.Knowledge.Embedder.Kind = "none"
	}
	if sp.Egress.Kind == "" {
		sp.Egress.Kind = "none"
	}
	if sp.Execution.MaxDepth == 0 {
		sp.Execution.MaxDepth = 4
	}
	if sp.Execution.MaxFanOut == 0 {
		sp.Execution.MaxFanOut = 16
	}
	// One run calling another needs two places, and one is always kept for
	// the runs that answer, so four lets calls nest without waiting.
	if sp.Execution.Concurrency == 0 {
		sp.Execution.Concurrency = 4
	}
	if sp.Server.DrainGrace == "" {
		sp.Server.DrainGrace = DefaultDrainGrace.String()
	}
	if sp.Server.ShutdownTimeout == "" {
		sp.Server.ShutdownTimeout = DefaultShutdownTimeout.String()
	}
	if sp.Rooms.CoalesceAbove == 0 {
		sp.Rooms.CoalesceAbove = rooms.DefaultCoalesceAbove
	}
	if sp.Rooms.TailInterval == "" {
		sp.Rooms.TailInterval = rooms.DefaultTailInterval.String()
	}
	if sp.Rooms.MaxRunsPerMinute == 0 {
		sp.Rooms.MaxRunsPerMinute = rooms.DefaultRoomRunsPerMinute
	}
	if sp.Rooms.MaxRunsPerPerson == 0 {
		sp.Rooms.MaxRunsPerPerson = rooms.DefaultPersonRuns
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
	if err := sp.Branding.check(true); err != nil {
		return fmt.Errorf("system: %w", err)
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
	}{{SlotAuth, sp.Auth.Slot}, {SlotStores, sp.Stores}, {SlotBus, sp.Bus}, {SlotBlobs, sp.Blobs}, {SlotEmbedder, sp.Knowledge.Embedder}, {SlotDeploy, sp.Deploy}, {SlotSandbox, sp.Sandbox}, {SlotEgress, sp.Egress}}
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
	if _, err := sp.Rooms.Config(); err != nil {
		return fmt.Errorf("system: %w", err)
	}
	if err := sp.Server.check(); err != nil {
		return fmt.Errorf("system: %w", err)
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
	return checkEmbedder(sp.Knowledge.Embedder)
}

// checkEmbedder refuses an embedder that could not be built at start.
func checkEmbedder(e Slot) error {
	switch e.Kind {
	case "hashing":
		if b, err := Block[HashingEmbedder](e); err != nil || b.Dimensions < 0 {
			return errors.New("system: knowledge.embedder: hashing dimensions must not be negative")
		}
	case "openai":
		if b, err := Block[OpenAIEmbedder](e); err != nil || b.Model == "" || b.Dimensions <= 0 {
			return errors.New("system: knowledge.embedder: openai needs model and dimensions")
		}
	case "vertex":
		if b, err := Block[VertexEmbedder](e); err != nil || b.Project == "" || b.Dimensions < 0 {
			return errors.New("system: knowledge.embedder: vertex needs project")
		}
	}
	return nil
}

// builtinBlock is the block type of each built-in kind, so a built-in is
// checked when the file is read.
func builtinBlock(slot, kind string) (any, bool) {
	switch slot + "." + kind {
	case "auth.dev", "auth.tailscale", "stores.memory", "bus.memory", "blobs.memory", "deploy.local", "sandbox.unisolated", "embedder.none", "egress.none":
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
	case "blobs.gcs":
		return &GCS{}, true
	case "embedder.hashing":
		return &HashingEmbedder{}, true
	case "embedder.openai":
		return &OpenAIEmbedder{}, true
	case "embedder.vertex":
		return &VertexEmbedder{}, true
	case "models.anthropic", "models.openai", "models.google":
		return &ModelBlock{}, true
	case "connectors.bigquery":
		return &BigQueryConnector{}, true
	case "tools.mcp":
		return &MCP{}, true
	case "sandbox.docker":
		return &DockerSandbox{}, true
	case "sandbox.local":
		return &LocalSandbox{}, true
	case "egress.gate":
		return &EgressGate{}, true
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
	case *DockerSandbox:
		if b.Runtime != "" && !runtimeName.MatchString(b.Runtime) {
			return fmt.Errorf("%s: runtime %q is not a runtime's name", where, b.Runtime)
		}
		if b.Warm < 0 || b.Max < 0 || (b.Max > 0 && b.Warm > b.Max) {
			return fmt.Errorf("%s: warm and max are counts, and warm is at most max", where)
		}
		if err := checkSandboxHost(b.Host, b.TLSDir); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
	case *EgressGate:
		for name, addr := range map[string]string{"listen": b.Listen, "address": b.Address} {
			if addr == "" {
				continue
			}
			host, port, err := net.SplitHostPort(addr)
			if err == nil {
				_, err = strconv.ParseUint(port, 10, 16)
			}
			if err != nil || strings.ContainsAny(host, " /@") || name == "address" && (host == "" || port == "0") {
				return fmt.Errorf("%s: %s %q is not host:port", where, name, addr)
			}
		}
		if b.Address == "" {
			if host, _, _ := net.SplitHostPort(b.Listen); b.Listen != "" && (host == "" || net.ParseIP(host).IsUnspecified()) {
				return fmt.Errorf("%s: listen %q is every address, so address must say which one instances reach", where, b.Listen)
			}
		}
		if b.Network != "" && !runtimeName.MatchString(b.Network) {
			return fmt.Errorf("%s: network %q is not a Docker network's name", where, b.Network)
		}
	case *BigQueryConnector:
		if b.Project == "" {
			return fmt.Errorf("%s: project is required", where)
		}
		if b.MaxBytesBilled < 0 || b.MaxRows < 0 {
			return fmt.Errorf("%s: max_bytes_billed and max_rows are not negative", where)
		}
		if b.Timeout != "" {
			if d, err := time.ParseDuration(b.Timeout); err != nil || d <= 0 {
				return fmt.Errorf("%s: timeout is a duration, such as 60s", where)
			}
		}
	case *GCS:
		if b.Bucket == "" {
			return fmt.Errorf("%s: bucket is required", where)
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

// checkSandboxHost checks a docker sandbox's host and tls_dir. A daemon on
// plain TCP is root on its machine for anyone who reaches it, so tcp://
// needs tls_dir.
func checkSandboxHost(host, tlsDir string) error {
	if host == "" {
		if tlsDir != "" {
			return errors.New("tls_dir needs a host")
		}
		return nil
	}
	scheme, rest, _ := strings.Cut(host, "://")
	switch {
	case rest == "" || strings.ContainsAny(host, " \t\n"):
		return fmt.Errorf("host %q is not a daemon address", host)
	case scheme == "tcp" && tlsDir == "":
		return fmt.Errorf("host %q is tcp://, which needs tls_dir", host)
	case scheme != "tcp" && scheme != "ssh" && scheme != "unix":
		return fmt.Errorf("host %q: want tcp://, ssh:// or unix://", host)
	case tlsDir != "" && !filepath.IsAbs(tlsDir):
		return fmt.Errorf("tls_dir %q is not an absolute path", tlsDir)
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
