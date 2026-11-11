package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/mcptools"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/catalog/bigquery"
	"github.com/urmzd/humagen/pkg/catalog/snowflake"
	"github.com/urmzd/humagen/pkg/credentials"
)

// A warehouse is a catalog connector whose rows stay where they are: its
// schema is crawled with its owner's login, and every data question is a
// read-only query run with the reader's own login, so the warehouse's own
// grants, row access and masking decide what each person reads.

// Login states a connector shows the person reading it.
const (
	// LoginConnected is a person with their own login to the warehouse.
	LoginConnected = "connected"
	// LoginMissing is a person who has not given one yet.
	LoginMissing = "missing"
	// LoginUnavailable is a warehouse whose sign-in this deployment has not
	// configured.
	LoginUnavailable = "unavailable"
)

// SnowflakeSetupInput is a managed Snowflake connector: where it reads and
// its limits. It holds no credential: each person gives their own login.
type SnowflakeSetupInput struct {
	Name      string `json:"name"`
	Account   string `json:"account"`
	Database  string `json:"database"`
	Warehouse string `json:"warehouse"`
	// Role is the role the owner's crawl uses. Empty is their default role.
	// Queries always use each reader's own default role.
	Role string `json:"role,omitempty"`
	// TokenType is how logins are given: OAUTH (default) or
	// PROGRAMMATIC_ACCESS_TOKEN.
	TokenType      string `json:"token_type,omitempty"`
	MaxRows        int    `json:"max_rows,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	RefreshHours   int    `json:"refresh_hours,omitempty"`
}

// CatalogLoginInput is a person's own login to a warehouse: a reference
// under a start the workspace lists, or, where the workspace allows it, the
// token itself, kept in the secrets store.
type CatalogLoginInput struct {
	Ref   string `json:"ref,omitempty"`
	Value string `json:"value,omitempty"`
}

var snowflakeAccount = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,200}$`)
var snowflakeIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,254}$`)

func validateSnowflakeSetup(in SnowflakeSetupInput) (SnowflakeSetupInput, error) {
	in.Name, in.Account, in.Database, in.Warehouse, in.Role = strings.TrimSpace(in.Name), strings.TrimSpace(in.Account), strings.TrimSpace(in.Database), strings.TrimSpace(in.Warehouse), strings.TrimSpace(in.Role)
	if in.TokenType == "" {
		in.TokenType = snowflake.TokenOAuth
	}
	if in.MaxRows == 0 {
		in.MaxRows = 200
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 60
	}
	if in.RefreshHours == 0 {
		in.RefreshHours = 24
	}
	switch {
	case !catalog.ValidName(in.Name):
		return in, fmt.Errorf("%w: a connector name is lower case letters, digits and dashes", catalog.ErrInvalid)
	case !snowflakeAccount.MatchString(in.Account) || strings.Contains(in.Account, ".."):
		return in, fmt.Errorf("%w: the account is its identifier, such as myorg-myaccount", catalog.ErrInvalid)
	case !snowflakeIdentifier.MatchString(in.Database) || !snowflakeIdentifier.MatchString(in.Warehouse) || (in.Role != "" && !snowflakeIdentifier.MatchString(in.Role)):
		return in, fmt.Errorf("%w: database, warehouse and role are Snowflake identifiers", catalog.ErrInvalid)
	case in.TokenType != snowflake.TokenOAuth && in.TokenType != snowflake.TokenProgrammatic:
		return in, fmt.Errorf("%w: token_type is OAUTH or PROGRAMMATIC_ACCESS_TOKEN", catalog.ErrInvalid)
	case in.MaxRows < 1 || in.MaxRows > 10000 || in.TimeoutSeconds < 1 || in.TimeoutSeconds > 600 || in.RefreshHours < 1 || in.RefreshHours > 720:
		return in, fmt.Errorf("%w: max_rows is 1 to 10000, timeout_seconds 1 to 600 and refresh_hours 1 to 720", catalog.ErrInvalid)
	}
	return in, nil
}

type snowflakeDefinition struct {
	Snowflake SnowflakeSetupInput `json:"snowflake"`
}

// CreateSnowflakeConnector adds a managed Snowflake connector owned by who.
// Its schema is crawled once its owner gives their own login.
func (p *Platform) CreateSnowflakeConnector(who auth.Principal, input SnowflakeSetupInput) (catalog.Connector, error) {
	if who.Kind != auth.User || who.Subject == "" || !who.Can(auth.KnowledgeWrite) {
		return catalog.Connector{}, auth.ErrForbidden
	}
	in, err := validateSnowflakeSetup(input)
	if err != nil {
		return catalog.Connector{}, err
	}
	raw, err := json.Marshal(snowflakeDefinition{Snowflake: in})
	if err != nil {
		return catalog.Connector{}, err
	}
	return p.CatalogStore.CreateManaged(who.Tenant, catalog.Connector{Name: in.Name, System: snowflake.System, Owner: who.Subject}, raw)
}

func (p *Platform) managedSnowflakeDefinition(tenant, name string) (catalog.Connector, SnowflakeSetupInput, error) {
	c, err := p.CatalogStore.Connector(tenant, name)
	if err != nil {
		return c, SnowflakeSetupInput{}, err
	}
	if c.System != snowflake.System || c.Owner == "" {
		return c, SnowflakeSetupInput{}, catalog.ErrInvalid
	}
	raw, err := p.CatalogStore.ManagedDefinition(tenant, name)
	if err != nil {
		return c, SnowflakeSetupInput{}, err
	}
	var def snowflakeDefinition
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&def); err != nil {
		return c, SnowflakeSetupInput{}, catalog.ErrInvalid
	}
	in, err := validateSnowflakeSetup(def.Snowflake)
	if err != nil || in.Name != name {
		return c, SnowflakeSetupInput{}, catalog.ErrInvalid
	}
	return c, in, nil
}

// managedSnowflake is a managed Snowflake connector. Its owner's login
// crawls the schema and answers the owner's own queries; a reader's query
// runs with the reader's login.
type managedSnowflake struct {
	p            *Platform
	tenant, name string
}

func (m managedSnowflake) client(token func(context.Context) (string, error), role bool) (*snowflake.Connector, error) {
	_, in, err := m.p.managedSnowflakeDefinition(m.tenant, m.name)
	if err != nil {
		return nil, err
	}
	cfg := snowflake.Config{Account: in.Account, Database: in.Database, Warehouse: in.Warehouse, TokenType: in.TokenType, TokenSource: token, MaxRows: in.MaxRows, Timeout: time.Duration(in.TimeoutSeconds) * time.Second}
	if role {
		cfg.Role = in.Role
	}
	return snowflake.New(cfg)
}

func (m managedSnowflake) owner() (string, error) {
	c, err := m.p.CatalogStore.Connector(m.tenant, m.name)
	if err != nil {
		return "", err
	}
	if !m.p.invitationReader(m.tenant, c.Owner).Can(auth.KnowledgeRead) {
		return "", auth.ErrForbidden
	}
	return c.Owner, nil
}

func (m managedSnowflake) Crawl(ctx context.Context) ([]catalog.Entry, error) {
	owner, err := m.owner()
	if err != nil {
		return nil, err
	}
	client, err := m.client(m.p.loginToken(m.tenant, owner, catalog.Connector{Name: m.name, System: snowflake.System}), true)
	if err != nil {
		return nil, err
	}
	return client.Crawl(ctx)
}

func (m managedSnowflake) Query(ctx context.Context, sql string) (catalog.Result, error) {
	owner, err := m.owner()
	if err != nil {
		return catalog.Result{}, err
	}
	client, err := m.client(m.p.loginToken(m.tenant, owner, catalog.Connector{Name: m.name, System: snowflake.System}), true)
	if err != nil {
		return catalog.Result{}, err
	}
	return client.Query(ctx, sql)
}

func (m managedSnowflake) QueryAs(ctx context.Context, token func(context.Context) (string, error), sql string) (catalog.Result, error) {
	client, err := m.client(token, false)
	if err != nil {
		return catalog.Result{}, err
	}
	return client.QueryAs(ctx, token, sql)
}

// QueryAs runs a managed BigQuery connector's query with the reader's own
// token, whatever credential its owner crawls with.
func (m managedBigQuery) QueryAs(ctx context.Context, token func(context.Context) (string, error), sql string) (catalog.Result, error) {
	_, def, err := m.p.managedBigQueryDefinition(m.tenant, m.name)
	if err != nil {
		return catalog.Result{}, err
	}
	in := def.Input
	client, err := bigquery.New(bigquery.Config{Project: in.Project, Datasets: in.Datasets, Location: in.Location, MaxBytesBilled: in.MaxBytesBilled, MaxRows: in.MaxRows, Timeout: time.Duration(in.TimeoutSeconds) * time.Second, TokenSource: token})
	if err != nil {
		return catalog.Result{}, err
	}
	return client.QueryAs(ctx, token, sql)
}

func (p *Platform) managedSnowflakeConnector(tenant, name string) (CatalogConnector, bool) {
	c, in, err := p.managedSnowflakeDefinition(tenant, name)
	if err != nil {
		return CatalogConnector{}, false
	}
	impl := managedSnowflake{p: p, tenant: tenant, name: name}
	return CatalogConnector{Tenant: tenant, Connector: c, Refresh: time.Duration(in.RefreshHours) * time.Hour, Crawler: impl, Querier: impl, Reader: impl}, true
}

// catalogLoginID is the credential a person's own login to a warehouse is
// kept under. Its prefix keeps it from ever being offered to a tool server.
func catalogLoginID(c catalog.Connector) string {
	hash := sha256.Sum256([]byte(c.Name + "\x00" + c.System))
	return "hcat-" + hex.EncodeToString(hash[:13])
}

// typedLogin reports whether a warehouse takes a login a person types in,
// rather than one they connect with OAuth.
func typedLogin(system string) bool { return system == snowflake.System }

// visibleConnector is a connector who may see, or not found.
func (p *Platform) visibleConnector(who auth.Principal, name string) (catalog.Connector, error) {
	c, err := p.CatalogStore.Connector(who.Tenant, name)
	if err != nil || who.Kind != auth.User || !who.Can(auth.KnowledgeRead) || !c.VisibleTo(who.Subject, p.Seer(who).Project) {
		return catalog.Connector{}, fmt.Errorf("%w: connector %q", catalog.ErrNotFound, name)
	}
	return c, nil
}

// SetCatalogLogin keeps who's own login to a warehouse they can see. It is
// theirs alone: nobody else's query ever uses it.
func (p *Platform) SetCatalogLogin(who auth.Principal, name string, in CatalogLoginInput) error {
	c, err := p.visibleConnector(who, name)
	if err != nil {
		return err
	}
	if !typedLogin(c.System) {
		return fmt.Errorf("%w: %s signs in with its connection, not a typed login", catalog.ErrInvalid, c.System)
	}
	in.Ref, in.Value = strings.TrimSpace(in.Ref), strings.TrimSpace(in.Value)
	switch {
	case (in.Ref == "") == (in.Value == ""):
		return fmt.Errorf("%w: give a reference or a token", catalog.ErrInvalid)
	case in.Ref != "" && !p.refAllowed(who.Tenant, who.Subject, in.Ref):
		return errPersonRef
	case in.Value != "" && !p.cfg.CredentialValues[who.Tenant]:
		return fmt.Errorf("%w: this workspace keeps references only; give a reference to your token", catalog.ErrInvalid)
	}
	_, err = p.Credentials.Put(who.Tenant, who.Subject, catalogLoginID(c), credentials.Input{Ref: in.Ref, Value: in.Value}, p.now())
	return err
}

// DeleteCatalogLogin forgets who's own login to a warehouse.
func (p *Platform) DeleteCatalogLogin(who auth.Principal, name string) error {
	c, err := p.visibleConnector(who, name)
	if err != nil {
		return err
	}
	if err := p.Credentials.Delete(who.Tenant, who.Subject, catalogLoginID(c)); err != nil && !errors.Is(err, credentials.ErrNotFound) {
		return err
	}
	return nil
}

// loginState is whether person has their own login to a warehouse.
func (p *Platform) loginState(tenant, person string, c catalog.Connector) string {
	switch {
	case typedLogin(c.System):
		if _, ok, err := p.Credentials.Get(tenant, person, catalogLoginID(c)); err == nil && ok {
			return LoginConnected
		}
		return LoginMissing
	case c.System == bigquery.System:
		if p.ConnectionAuth == nil || !p.ConnectionAuth.Available("bigquery") {
			return LoginUnavailable
		}
		if s, err := p.ConnectionAuth.Status(tenant, person, "bigquery"); err == nil && s.Connected {
			return LoginConnected
		}
		return LoginMissing
	}
	return ""
}

// errNoLogin is a reader with no login of their own to a warehouse.
var errNoLogin = errors.New("connect your own login to this warehouse first")

// loginToken reads person's own token to a warehouse, per call: a typed
// login (a reference the workspace allows, or a stored value it lets people
// keep), or their own OAuth connection. Never anyone else's.
func (p *Platform) loginToken(tenant, person string, c catalog.Connector) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		if person == "" {
			return "", errNoLogin
		}
		if c.System == bigquery.System {
			token, err := p.ConnectionAuth.Token(ctx, tenant, person, "bigquery")
			if err != nil || token == "" {
				return "", errNoLogin
			}
			return token, nil
		}
		if !typedLogin(c.System) {
			return "", errNoLogin
		}
		id := catalogLoginID(c)
		cred, ok, err := p.Credentials.Get(tenant, person, id)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", errNoLogin
		}
		if cred.Kind == credentials.Value {
			if !p.cfg.CredentialValues[tenant] {
				return "", errNoLogin
			}
			v, ok, err := p.Credentials.Value(tenant, person, id)
			if err != nil || !ok || v == "" {
				return "", errNoLogin
			}
			return v, nil
		}
		if !p.refAllowed(tenant, person, cred.Ref) {
			return "", errNoLogin
		}
		resolver := p.cfg.Ext.Resolver
		if resolver == nil {
			resolver = mcptools.EnvResolver()
		}
		secret, err := resolver.Resolve(ctx, cred.Ref)
		if err != nil || secret.Empty() {
			return "", errNoLogin
		}
		return secret.Reveal(), nil
	}
}
