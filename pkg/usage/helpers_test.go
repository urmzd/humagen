package usage

import (
	"time"

	"github.com/urmzd/humagen/pkg/policy"
)

// Reads that fail the test's process: a ledger in a test reads a store that
// does not fail unless the test makes it.

func (l *Ledger) used(tenant string, scope policy.Scope, since time.Time) policy.Usage {
	u, err := l.Usage(tenant, scope, since)
	if err != nil {
		panic(err)
	}
	return u
}

func (l *Ledger) all(tenant string) []Entry {
	out, err := l.Entries(tenant)
	if err != nil {
		panic(err)
	}
	return out
}

func (l *Ledger) notices(tenant string, limit int) []Entry {
	out, err := l.Notices(tenant, limit)
	if err != nil {
		panic(err)
	}
	return out
}

func (l *Ledger) runSpend(tenant, run string) Spend {
	out, err := l.RunSpend(tenant, run)
	if err != nil {
		panic(err)
	}
	return out
}

func (l *Ledger) granted(tenant, run string, scope policy.Scope, since time.Time) policy.Grant {
	out, err := l.Granted(tenant, run, scope, since)
	if err != nil {
		panic(err)
	}
	return out
}

func (l *Ledger) grants(tenant, run string, scope policy.Scope) int {
	n, err := l.Grants(tenant, run, scope)
	if err != nil {
		panic(err)
	}
	return n
}
