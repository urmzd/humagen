package github

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/urmzd/saige/flow/connector"
)

// PlaceReader reads an issue or pull request as a run on it reads its own
// repository: through the transport's github_read_context, bound to the
// issue or pull request itself. A redirect is followed only when it names
// a repository by owner and name and that repository is this one or passes
// the read grants (Config.Owners, ReadRefused, ReadRepositories, MayRead),
// as the transport decides for a run's own read. A redirect that names a
// repository by id, as GitHub's for a renamed or transferred repository do,
// is not followed. This holds for an API that follows redirects, such as
// one with a personal token; the GitHub App client follows none for any
// read (appReads), so a redirected read through it fails with
// ErrReadRedirected.
// It is for the reads the host makes for a run on that place; whether the
// place is readable at all is the caller's to decide.
func (c *Connector) PlaceReader() ContextAPI { return placeReader{c: c} }

type placeReader struct{ c *Connector }

func (r placeReader) ReadContext(ctx context.Context, ref Ref, page int) (Context, error) {
	for _, a := range r.c.Connector.Actions() {
		if a.Name != "github_read_context" {
			continue
		}
		args, err := json.Marshal(map[string]int{"page": max(page, 1)})
		if err != nil {
			return Context{}, err
		}
		result, err := a.Invoke(ctx, refAddress(ref, "issues"), args)
		if err != nil {
			return Context{}, err
		}
		var out Context
		if err := json.Unmarshal(result.Data, &out); err != nil {
			return Context{}, fmt.Errorf("github: read context for %s: %w", ref, err)
		}
		return out, nil
	}
	return Context{}, fmt.Errorf("%w: github context reads", connector.ErrUnsupported)
}
