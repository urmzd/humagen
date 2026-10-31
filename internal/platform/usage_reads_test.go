package platform_test

import (
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/usage"
)

// Reads of the usage ledger that fail the test's process: the ledger in a
// test reads a store that does not fail unless the test makes it.

func ledgerUsage(l *usage.Ledger, tenant string, scope policy.Scope, since time.Time) policy.Usage {
	u, err := l.Usage(tenant, scope, since)
	if err != nil {
		panic(err)
	}
	return u
}

func usageOf(p *platform.Platform, tenant string, scope policy.Scope, since time.Time) policy.Usage {
	return ledgerUsage(p.Usage, tenant, scope, since)
}

func entriesOf(p *platform.Platform, tenant string) []usage.Entry {
	out, err := p.Usage.Entries(tenant)
	if err != nil {
		panic(err)
	}
	return out
}

func noticesOf(p *platform.Platform, tenant string, limit int) []usage.Entry {
	out, err := p.Usage.Notices(tenant, limit)
	if err != nil {
		panic(err)
	}
	return out
}

func runSpendOf(p *platform.Platform, tenant, run string) usage.Spend {
	out, err := p.Usage.RunSpend(tenant, run)
	if err != nil {
		panic(err)
	}
	return out
}

func grantedOf(p *platform.Platform, tenant, run string, scope policy.Scope, since time.Time) policy.Grant {
	out, err := p.Usage.Granted(tenant, run, scope, since)
	if err != nil {
		panic(err)
	}
	return out
}
