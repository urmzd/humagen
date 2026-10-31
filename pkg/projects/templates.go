package projects

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

//go:embed templates.json
var templateData []byte

// Template is configuration supplied by a file or the embedding application's
// Config.ProjectTemplates slot. It contains no agents, memory or live bindings.
type Template struct {
	Name         string    `json:"name"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Kind         Kind      `json:"kind"`
	Initiative   bool      `json:"initiative"`
	Instructions string    `json:"instructions,omitempty"`
	Channels     []Channel `json:"channels,omitempty"`
	Skills       []Skill   `json:"skills,omitempty"`
}

func DefaultTemplates() []Template {
	var out []Template
	if err := json.Unmarshal(templateData, &out); err != nil {
		panic("projects: invalid embedded templates: " + err.Error())
	}
	return out
}
func ValidateTemplates(templates []Template) error {
	if len(templates) > 16 {
		return fmt.Errorf("%w: at most 16 project templates", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, template := range templates {
		if !validKindName(Kind(template.Name)) || seen[template.Name] || template.Title == "" || template.Kind != Team && template.Kind != Account || template.Initiative && template.Kind != Team || len(template.Channels) > MaxChannels {
			return fmt.Errorf("%w: invalid project template", ErrInvalid)
		}
		sample := Project{Name: "template-validation", Title: template.Title, Description: template.Description, Kind: template.Kind, Template: template.Name, Initiative: template.Initiative, Instructions: template.Instructions, Skills: template.Skills, Members: []Entry{{Who: "creator", Role: Owner}}}
		if err := sample.validate(); err != nil {
			return err
		}
		seen[template.Name] = true
		seenChannels := map[string]bool{}
		for _, channel := range template.Channels {
			if seenChannels[channel.Name] {
				return fmt.Errorf("%w: duplicate template channel", ErrInvalid)
			}
			seenChannels[channel.Name] = true
			channel.Project = "template-validation"
			if err := channel.validate(); err != nil {
				return err
			}
			if channel.Slack != nil || channel.Default != "" {
				return fmt.Errorf("%w: a template carries no live binding or agent default", ErrInvalid)
			}
		}
	}
	return nil
}

// CreateTemplate writes the project, channels and skills together. A failed
// write cannot leave a project claiming a template it did not receive.
func (s *Store) CreateTemplate(tenantName string, pr Project, template Template, w Writer, at time.Time) (Project, error) {
	if err := ValidateTemplates([]Template{template}); err != nil {
		return Project{}, err
	}
	if pr.Kind != "" && pr.Kind != template.Kind {
		return Project{}, fmt.Errorf("%w: template kind is fixed", ErrInvalid)
	}
	pr.Template, pr.Initiative = template.Name, template.Initiative
	pr.Kind = cmp.Or(pr.Kind, template.Kind)
	if pr.Instructions == "" {
		pr.Instructions = template.Instructions
	}
	pr.Skills = template.Skills
	pr = pr.clone()
	pr.Managed, pr.FileOwns = "", nil
	pr.Agents = pr.Agents.canonical()
	pr.Name = strings.TrimSpace(pr.Name)
	if pr.IsRoot() || pr.Archived {
		return Project{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return retry(func() (Project, error) { return s.create(tenantName, pr, w, at, w.By, true, template.Channels...) })
}
