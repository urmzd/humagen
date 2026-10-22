package platform

import (
	"maps"
	"testing"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/warp/pkg/runs"
)

// A run's pinned sources reach its revision under either key, and the key is
// taken out of the request's copy of the meta. The run's tag is never
// changed.
func TestPinnedSourcesReachTheRevisionUnderEitherKey(t *testing.T) {
	for _, key := range []string{runs.MetaSources, runs.MetaSourcesV0} {
		tagMeta := map[string]string{key: "playbook,guide", "warp.project": "Field"}
		before := maps.Clone(tagMeta)
		req := withSources(runtime.Request{Meta: tagMeta, Revision: agentrt.Revision{}})
		var names []string
		for _, s := range req.Revision.Spec.Sources {
			names = append(names, s.Name+"="+s.URI)
		}
		if len(names) != 2 || names[0] != "playbook=knowledge://playbook" || names[1] != "guide=knowledge://guide" {
			t.Errorf("pinned under %q, the revision's sources = %v", key, names)
		}
		if _, ok := req.Meta[key]; ok {
			t.Errorf("pinned under %q, the request's meta keeps it", key)
		}
		if req.Meta["warp.project"] != "Field" || !maps.Equal(tagMeta, before) {
			t.Errorf("pinned under %q, the rest of the meta or the tag changed: %v, %v", key, req.Meta, tagMeta)
		}
	}
}
