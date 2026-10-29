package spec

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// Welcome is the announcement a workspace's people find first in its inbox.
type Welcome struct {
	Title string `json:"title,omitempty"`
	Text  string `json:"text,omitempty"`
}

// Starter is a suggestion on an empty conversation: a pick adds the agent
// and writes the start of a message.
type Starter struct {
	Agent string `json:"agent"`
	Label string `json:"label"`
	Text  string `json:"text"`
}

// EmailCopy is what notification emails say. {name} is the product's name
// in each; {count} is the number of updates in Multiple.
type EmailCopy struct {
	Subject  string `json:"subject,omitempty"`
	Single   string `json:"single,omitempty"`
	Multiple string `json:"multiple,omitempty"`
	Footer   string `json:"footer,omitempty"`
}

// MaxStarters is the most suggestions an empty conversation shows.
const MaxStarters = 6

var (
	starterAgent = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	placeholder  = regexp.MustCompile(`\{[^{}]*\}`)
)

// copyKeys is copyKeyList as a set.
var copyKeys = func() map[string]bool {
	m := make(map[string]bool, len(copyKeyList))
	for _, k := range copyKeyList {
		m[k] = true
	}
	return m
}()

// CopyKeys are the console's own text, by key, that branding.copy may
// replace: every string in packages/components/src/copy.ts.
func CopyKeys() []string {
	return slices.Clone(copyKeyList)
}

func checkWelcome(w *Welcome) error {
	if w == nil {
		return nil
	}
	if err := checkText(w.Title, 120, false); err != nil {
		return fmt.Errorf("branding.welcome.title: %w", err)
	}
	if err := checkText(w.Text, 2000, true); err != nil {
		return fmt.Errorf("branding.welcome.text: %w", err)
	}
	return nil
}

func checkStarters(ss []Starter) error {
	if len(ss) > MaxStarters {
		return fmt.Errorf("branding.starters: at most %d", MaxStarters)
	}
	for i, s := range ss {
		if !starterAgent.MatchString(s.Agent) {
			return fmt.Errorf("branding.starters[%d].agent is an agent's name, in lower case", i)
		}
		if s.Label == "" || s.Text == "" {
			return fmt.Errorf("branding.starters[%d]: label and text are required", i)
		}
		if err := checkText(s.Label, 75, false); err != nil {
			return fmt.Errorf("branding.starters[%d].label: %w", i, err)
		}
		if err := checkText(s.Text, 2000, true); err != nil {
			return fmt.Errorf("branding.starters[%d].text: %w", i, err)
		}
	}
	return nil
}

func checkCopy(c map[string]string) error {
	for k, v := range c {
		if !copyKeys[k] {
			return fmt.Errorf("branding.copy: the console has no text called %q; `humagen config copy-keys` lists them", k)
		}
		if v == "" {
			return fmt.Errorf("branding.copy.%s is empty; leave it out to keep the console's own", k)
		}
		if err := checkText(v, 500, true); err != nil {
			return fmt.Errorf("branding.copy.%s: %w", k, err)
		}
	}
	return nil
}

func checkEmail(e *EmailCopy) error {
	if e == nil {
		return nil
	}
	for _, f := range []struct {
		name, v string
		allowed []string
		limit   int
	}{
		{"subject", e.Subject, []string{"{name}"}, 120},
		{"single", e.Single, []string{"{name}"}, 500},
		{"multiple", e.Multiple, []string{"{name}", "{count}"}, 500},
		{"footer", e.Footer, []string{"{name}"}, 500},
	} {
		if err := checkText(f.v, f.limit, f.name != "subject"); err != nil {
			return fmt.Errorf("branding.email.%s: %w", f.name, err)
		}
		for _, p := range placeholder.FindAllString(f.v, -1) {
			if !slices.Contains(f.allowed, p) {
				return fmt.Errorf("branding.email.%s: %s is not a placeholder here; it takes %s", f.name, p, strings.Join(f.allowed, " and "))
			}
		}
	}
	if e.Multiple != "" && !strings.Contains(e.Multiple, "{count}") {
		return errors.New("branding.email.multiple says how many updates: put {count} in it")
	}
	return nil
}

// mergeCopy is the workspace's text over the deployment's.
func mergeCopy(sys, ws Branding, out *Branding) {
	if ws.Welcome != nil {
		w := Welcome{}
		if sys.Welcome != nil {
			w = *sys.Welcome
		}
		if ws.Welcome.Title != "" {
			w.Title = ws.Welcome.Title
		}
		if ws.Welcome.Text != "" {
			w.Text = ws.Welcome.Text
		}
		out.Welcome = &w
	}
	if ws.Starters != nil {
		out.Starters = ws.Starters
	}
	if len(ws.Copy) > 0 {
		c := map[string]string{}
		for k, v := range sys.Copy {
			c[k] = v
		}
		for k, v := range ws.Copy {
			c[k] = v
		}
		out.Copy = c
	}
	if ws.Email != nil {
		e := EmailCopy{}
		if sys.Email != nil {
			e = *sys.Email
		}
		if ws.Email.Subject != "" {
			e.Subject = ws.Email.Subject
		}
		if ws.Email.Single != "" {
			e.Single = ws.Email.Single
		}
		if ws.Email.Multiple != "" {
			e.Multiple = ws.Email.Multiple
		}
		if ws.Email.Footer != "" {
			e.Footer = ws.Email.Footer
		}
		out.Email = &e
	}
}

// IsZero reports a block that sets nothing. starters: [] sets something:
// that no suggestions show.
func (b Branding) IsZero() bool { return reflect.ValueOf(b).IsZero() }
