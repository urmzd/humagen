package platform

import (
	"strings"

	"github.com/urmzd/humagen/pkg/operations"
)

func (p *Platform) operationPersonSubject(tenant, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if person, err := p.People.Get(tenant, value); err == nil {
		return person.Subject, nil
	}
	if person, err := p.People.ByHandle(tenant, strings.TrimPrefix(value, "@")); err == nil {
		return person.Subject, nil
	}
	return "", operations.ErrInvalid
}
