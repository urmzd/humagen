package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/catalog/bigquery"
	"github.com/urmzd/humagen/pkg/catalog/snowflake"
)

type BigQuerySetupCredential struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref,omitempty"`
}
type BigQuerySetupInput struct {
	Name           string                  `json:"name"`
	Project        string                  `json:"project"`
	Datasets       []string                `json:"datasets"`
	Location       string                  `json:"location,omitempty"`
	Credential     BigQuerySetupCredential `json:"credential"`
	MaxBytesBilled int64                   `json:"max_bytes_billed"`
	MaxRows        int                     `json:"max_rows"`
	TimeoutSeconds int                     `json:"timeout_seconds"`
	RefreshHours   int                     `json:"refresh_hours"`
}
type BigQueryADCScope struct {
	Project  string   `json:"project"`
	Datasets []string `json:"datasets"`
}
type BigQuerySetupView struct {
	OAuthConnectors []string           `json:"oauth_connectors"`
	OAuthAvailable  bool               `json:"oauth_available"`
	Connected       bool               `json:"connected"`
	ADC             []BigQueryADCScope `json:"adc"`
	CredentialRefs  []string           `json:"credential_refs"`
}

type bigQueryDefinition struct {
	Input   BigQuerySetupInput `json:"input"`
	Binding string             `json:"binding,omitempty"`
}

var bigQueryProject = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var bigQueryDataset = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
var bigQueryLocation = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

func validateBigQuerySetup(in BigQuerySetupInput) (BigQuerySetupInput, error) {
	in.Name, in.Project, in.Location = strings.TrimSpace(in.Name), strings.TrimSpace(in.Project), strings.TrimSpace(in.Location)
	if in.Credential.Kind == "" {
		in.Credential.Kind = "oauth"
	}
	if !catalog.ValidName(in.Name) || !bigQueryProject.MatchString(in.Project) || len(in.Datasets) < 1 || len(in.Datasets) > 50 || (in.Location != "" && !bigQueryLocation.MatchString(in.Location)) || in.MaxBytesBilled < 1 || in.MaxBytesBilled > 1<<40 || in.MaxRows < 1 || in.MaxRows > 10000 || in.TimeoutSeconds < 1 || in.TimeoutSeconds > 600 || in.RefreshHours < 1 || in.RefreshHours > 720 {
		return in, fmt.Errorf("%w: name, project, explicit datasets and bounded query limits are required", catalog.ErrInvalid)
	}
	total := 0
	for i, dataset := range in.Datasets {
		total += len(dataset)
		if len(dataset) > 1024 || !bigQueryDataset.MatchString(dataset) || slices.Contains(in.Datasets[:i], dataset) || total > 6000 {
			return in, fmt.Errorf("%w: dataset IDs must be unique and bounded", catalog.ErrInvalid)
		}
	}
	switch in.Credential.Kind {
	case "oauth", "adc":
		if in.Credential.Ref != "" {
			return in, fmt.Errorf("%w: OAuth and ADC do not accept a credential reference", catalog.ErrInvalid)
		}
	case "reference":
		if in.Credential.Ref == "" || len(in.Credential.Ref) > 512 {
			return in, fmt.Errorf("%w: an allowed credential reference is required", catalog.ErrInvalid)
		}
	default:
		return in, fmt.Errorf("%w: credential kind must be oauth, adc or reference", catalog.ErrInvalid)
	}
	in.Datasets = slices.Clone(in.Datasets)
	slices.Sort(in.Datasets)
	return in, nil
}

// ADC authority is inherited only from a deployment-declared connector in
// this tenant. Creating a connector cannot grant new server identity scope.
func (p *Platform) bigQueryADCScopes(tenant string) []BigQueryADCScope {
	out := []BigQueryADCScope{}
	for _, c := range p.cfg.Connectors {
		if c.Tenant != tenant || c.System != bigquery.System {
			continue
		}
		for _, impl := range []any{c.Crawler, c.Querier} {
			configured, ok := impl.(interface{ Config() bigquery.Config })
			if !ok {
				continue
			}
			cfg := configured.Config()
			if cfg.Credential != "" || cfg.TokenSource != nil {
				continue
			}
			datasets := append([]string{}, cfg.Datasets...)
			slices.Sort(datasets)
			if !slices.ContainsFunc(out, func(s BigQueryADCScope) bool { return s.Project == cfg.Project && slices.Equal(s.Datasets, datasets) }) {
				out = append(out, BigQueryADCScope{Project: cfg.Project, Datasets: datasets})
			}
		}
	}
	slices.SortFunc(out, func(a, b BigQueryADCScope) int {
		return strings.Compare(a.Project+strings.Join(a.Datasets, ","), b.Project+strings.Join(b.Datasets, ","))
	})
	return out
}

func (p *Platform) BigQuerySetup(who auth.Principal) (BigQuerySetupView, error) {
	out := BigQuerySetupView{OAuthConnectors: []string{}, ADC: []BigQueryADCScope{}, CredentialRefs: []string{}}
	if who.Kind != auth.User || who.Subject == "" || !who.Can(auth.KnowledgeWrite) {
		return out, auth.ErrForbidden
	}
	if p.ConnectionAuth != nil && p.ConnectionAuth.Available("bigquery") {
		out.OAuthAvailable = true
		status, err := p.ConnectionAuth.Status(who.Tenant, who.Subject, "bigquery")
		if err != nil {
			return out, err
		}
		out.Connected = status.Connected
	}
	if who.Can(auth.ClientsWrite) {
		out.ADC = p.bigQueryADCScopes(who.Tenant)
		out.CredentialRefs = append(out.CredentialRefs, p.ownStarts(who.Tenant, who.Subject)...)
	}
	connectors, err := p.CatalogStore.Connectors(who.Tenant)
	if err != nil {
		return out, err
	}
	for _, c := range connectors {
		if c.Owner != who.Subject || c.System != bigquery.System {
			continue
		}
		_, def, err := p.managedBigQueryDefinition(who.Tenant, c.Name)
		if errors.Is(err, catalog.ErrNotFound) {
			continue
		}
		if err != nil {
			return out, err
		}
		if def.Input.Credential.Kind == "oauth" {
			out.OAuthConnectors = append(out.OAuthConnectors, c.Name)
		}
	}
	return out, nil
}

func (p *Platform) bigQueryCredentialAllowed(who auth.Principal, in BigQuerySetupInput) bool {
	switch in.Credential.Kind {
	case "oauth":
		return p.ConnectionAuth != nil && p.ConnectionAuth.Available("bigquery")
	case "reference":
		return who.Can(auth.ClientsWrite) && p.refAllowed(who.Tenant, who.Subject, in.Credential.Ref)
	case "adc":
		return who.Can(auth.ClientsWrite) && slices.ContainsFunc(p.bigQueryADCScopes(who.Tenant), func(s BigQueryADCScope) bool {
			return s.Project == in.Project && (len(s.Datasets) == 0 || !slices.ContainsFunc(in.Datasets, func(d string) bool { return !slices.Contains(s.Datasets, d) }))
		})
	}
	return false
}

func (p *Platform) CreateBigQueryConnector(ctx context.Context, who auth.Principal, input BigQuerySetupInput) (catalog.Connector, error) {
	if who.Kind != auth.User || who.Subject == "" || !who.Can(auth.KnowledgeWrite) {
		return catalog.Connector{}, auth.ErrForbidden
	}
	in, err := validateBigQuerySetup(input)
	if err != nil {
		return catalog.Connector{}, err
	}
	if !p.bigQueryCredentialAllowed(who, in) {
		return catalog.Connector{}, auth.ErrForbidden
	}
	if err := ctx.Err(); err != nil {
		return catalog.Connector{}, err
	}
	def := bigQueryDefinition{Input: in}
	if in.Credential.Kind == "oauth" {
		// A unique target prevents a losing concurrent create from rebinding
		// an existing connector. Include the immutable resource definition.
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return catalog.Connector{}, err
		}
		raw, _ := json.Marshal(in)
		digest := sha256.Sum256(raw)
		def.Binding = "catalog-bigquery:" + hex.EncodeToString(digest[:16]) + ":" + hex.EncodeToString(nonce)
		if err := p.ConnectionAuth.Bind(who.Tenant, who.Subject, def.Binding, "bigquery"); err != nil {
			return catalog.Connector{}, auth.ErrForbidden
		}
	}
	raw, err := json.Marshal(def)
	if err != nil {
		return catalog.Connector{}, err
	}
	created, err := p.CatalogStore.CreateManaged(who.Tenant, catalog.Connector{Name: in.Name, System: bigquery.System, Owner: who.Subject}, raw)
	if err != nil && def.Binding != "" {
		_ = p.ConnectionAuth.Unbind(who.Tenant, who.Subject, def.Binding)
	}
	return created, err
}

func (p *Platform) managedBigQueryDefinition(tenant, name string) (catalog.Connector, bigQueryDefinition, error) {
	c, err := p.CatalogStore.Connector(tenant, name)
	if err != nil {
		return c, bigQueryDefinition{}, err
	}
	raw, err := p.CatalogStore.ManagedDefinition(tenant, name)
	if err != nil {
		return c, bigQueryDefinition{}, err
	}
	var def bigQueryDefinition
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&def); err != nil {
		return c, def, err
	}
	in, err := validateBigQuerySetup(def.Input)
	if err != nil || in.Name != name || c.System != bigquery.System || c.Owner == "" || (in.Credential.Kind == "oauth" && !strings.HasPrefix(def.Binding, "catalog-bigquery:")) || (in.Credential.Kind != "oauth" && def.Binding != "") {
		return c, def, catalog.ErrInvalid
	}
	def.Input = in
	if in.Credential.Kind == "oauth" {
		raw, _ := json.Marshal(in)
		digest := sha256.Sum256(raw)
		nonce, ok := strings.CutPrefix(def.Binding, "catalog-bigquery:"+hex.EncodeToString(digest[:16])+":")
		decoded, decodeErr := hex.DecodeString(nonce)
		if !ok || decodeErr != nil || len(decoded) != 16 || len(nonce) != 32 {
			return c, def, catalog.ErrInvalid
		}
	}
	return c, def, nil
}

func (p *Platform) ReconnectBigQueryConnector(ctx context.Context, who auth.Principal, name string) (catalog.Connector, error) {
	if who.Kind != auth.User || !who.Can(auth.KnowledgeWrite) {
		return catalog.Connector{}, auth.ErrForbidden
	}
	c, def, err := p.managedBigQueryDefinition(who.Tenant, name)
	if err != nil || c.Owner != who.Subject {
		return catalog.Connector{}, catalog.ErrNotFound
	}
	if def.Input.Credential.Kind != "oauth" || !p.bigQueryCredentialAllowed(who, def.Input) {
		return catalog.Connector{}, auth.ErrForbidden
	}
	if err := ctx.Err(); err != nil {
		return catalog.Connector{}, err
	}
	if err := p.ConnectionAuth.Bind(who.Tenant, who.Subject, def.Binding, "bigquery"); err != nil {
		return catalog.Connector{}, auth.ErrForbidden
	}
	return c, nil
}

type managedBigQuery struct {
	p            *Platform
	tenant, name string
}

func (m managedBigQuery) client() (*bigquery.Connector, string, error) {
	c, def, err := m.p.managedBigQueryDefinition(m.tenant, m.name)
	if err != nil {
		return nil, "", err
	}
	who := m.p.invitationReader(m.tenant, c.Owner)
	if !who.Can(auth.KnowledgeRead) || !m.p.bigQueryCredentialAllowed(who, def.Input) {
		return nil, "", auth.ErrForbidden
	}
	in := def.Input
	cfg := bigquery.Config{Project: in.Project, Datasets: in.Datasets, Location: in.Location, MaxBytesBilled: in.MaxBytesBilled, MaxRows: in.MaxRows, Timeout: time.Duration(in.TimeoutSeconds) * time.Second}
	marker := ""
	switch in.Credential.Kind {
	case "oauth":
		provider, grant, err := m.p.ConnectionAuth.Binding(m.tenant, c.Owner, def.Binding)
		if err != nil || provider != "bigquery" || grant == "" {
			return nil, "", auth.ErrForbidden
		}
		marker = grant
		cfg.TokenSource = func(ctx context.Context) (string, error) {
			current := m.p.invitationReader(m.tenant, c.Owner)
			if !current.Can(auth.KnowledgeRead) {
				return "", auth.ErrForbidden
			}
			return m.p.ConnectionAuth.BoundToken(ctx, m.tenant, c.Owner, def.Binding, "bigquery")
		}
	case "reference":
		cfg.Credential = in.Credential.Ref
	}
	client, err := bigquery.New(cfg)
	return client, marker, err
}
func (m managedBigQuery) unchanged(marker string) error {
	_, current, err := m.client()
	if err != nil {
		return err
	}
	if current != marker {
		return auth.ErrForbidden
	}
	return nil
}
func (m managedBigQuery) Crawl(ctx context.Context) ([]catalog.Entry, error) {
	client, marker, err := m.client()
	if err != nil {
		return nil, err
	}
	rows, err := client.Crawl(ctx)
	if err == nil {
		err = m.unchanged(marker)
	}
	return rows, err
}
func (m managedBigQuery) Query(ctx context.Context, query string) (catalog.Result, error) {
	_, definition, err := m.p.managedBigQueryDefinition(m.tenant, m.name)
	if err != nil {
		return catalog.Result{}, err
	}
	// The SDK uses jobs.insert, which the read-only OAuth scope does not
	// authorize. Do not broaden consent or expose an unusable live capability.
	if definition.Input.Credential.Kind == "oauth" {
		return catalog.Result{}, auth.ErrForbidden
	}
	client, marker, err := m.client()
	if err != nil {
		return catalog.Result{}, err
	}
	result, err := client.Query(ctx, query)
	if err == nil {
		err = m.unchanged(marker)
	}
	if err != nil {
		return catalog.Result{}, err
	}
	return result, nil
}

func (p *Platform) managedCatalogConnector(tenant, name string) (CatalogConnector, bool) {
	if c, err := p.CatalogStore.Connector(tenant, name); err == nil && c.System == snowflake.System {
		return p.managedSnowflakeConnector(tenant, name)
	}
	c, def, err := p.managedBigQueryDefinition(tenant, name)
	if err != nil {
		return CatalogConnector{}, false
	}
	impl := managedBigQuery{p: p, tenant: tenant, name: name}
	out := CatalogConnector{Tenant: tenant, Connector: c, Refresh: time.Duration(def.Input.RefreshHours) * time.Hour, Crawler: impl, Reader: impl}
	if def.Input.Credential.Kind != "oauth" {
		out.Querier = impl
	}
	return out, true
}

func (p *Platform) refreshManagedCatalog() error {
	return p.CatalogStore.EachTenant(func(tenant string) error {
		cs, err := p.CatalogStore.Connectors(tenant)
		if err != nil {
			return err
		}
		for _, c := range cs {
			if _, err := p.CatalogStore.ManagedDefinition(tenant, c.Name); errors.Is(err, catalog.ErrNotFound) {
				continue
			} else if err != nil {
				return err
			}
			managed, ok := p.managedCatalogConnector(tenant, c.Name)
			if !ok {
				return catalog.ErrInvalid
			}
			p.catalog.mu.Lock()
			p.catalog.declared[[2]string{tenant, c.Name}] = managed
			p.catalog.mu.Unlock()
		}
		return nil
	})
}
