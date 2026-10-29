package platform

import "strings"

// Wording is a workspace's product name and the text its branding replaces
// (docs/spec.md#branding). An empty field is Humagen's own.
type Wording struct {
	// Name is the product's name, which Humagen's own text says in place of
	// "Humagen" and a replacement says for {name}.
	Name          string
	WelcomeTitle  string
	WelcomeText   string
	EmailSubject  string
	EmailSingle   string
	EmailMultiple string
	EmailFooter   string
}

// wording is the tenant's text, every field filled: its branding's, else
// Humagen's under the product's name.
func (p *Platform) wording(tenant string) Wording {
	var w Wording
	if p.cfg.Wording != nil {
		w = p.cfg.Wording(tenant)
	}
	name := w.Name
	if name == "" {
		name = SystemDisplay
	}
	pick := func(own, humagen string) string {
		if own == "" {
			return strings.ReplaceAll(humagen, "Humagen", name)
		}
		return strings.ReplaceAll(own, "{name}", name)
	}
	return Wording{
		Name:          name,
		WelcomeTitle:  pick(w.WelcomeTitle, welcomeTitle),
		WelcomeText:   pick(w.WelcomeText, welcomeText),
		EmailSubject:  pick(w.EmailSubject, emailSubject),
		EmailSingle:   pick(w.EmailSingle, emailSingle),
		EmailMultiple: pick(w.EmailMultiple, emailMultiple),
		EmailFooter:   pick(w.EmailFooter, emailFooter),
	}
}
