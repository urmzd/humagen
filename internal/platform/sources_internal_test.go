package platform

import (
	"maps"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"

	"github.com/urmzd/humagen/pkg/runs"
)

// A run's pinned sources reach its revision under either key, and the key is
// taken out of the request's copy of the meta. The run's tag is never
// changed.
func TestPinnedSourcesReachTheRevisionUnderEitherKey(t *testing.T) {
	for _, key := range []string{runs.MetaSources, runs.MetaSourcesV0} {
		tagMeta := map[string]string{key: "playbook,guide", "warp.project": "Field"}
		before := maps.Clone(tagMeta)
		req := withSources(runtime.Request{Meta: tagMeta, Definition: native.Revision{}})
		var names []string
		for _, s := range req.Definition.Spec.Sources {
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

func TestAForkSourceSelectionNarrowsAgentAndInheritedSources(t *testing.T) {
	for _, selection := range []string{"", "guide"} {
		req := runtime.Request{Meta: map[string]string{metaForkSources: selection, runs.MetaSources: selection}, Definition: native.Revision{Spec: native.Spec{Sources: []native.Source{{Name: "secret", URI: "knowledge://secret"}, {Name: "guide", URI: "knowledge://guide"}}}}}
		got := withSources(req)
		if selection == "" && len(got.Definition.Spec.Sources) != 0 {
			t.Fatalf("empty selection kept agent sources: %+v", got.Definition.Spec.Sources)
		}
		if selection == "guide" && (len(got.Definition.Spec.Sources) != 1 || got.Definition.Spec.Sources[0].Name != "guide") {
			t.Fatalf("selection did not confine sources: %+v", got.Definition.Spec.Sources)
		}
		if _, ok := got.Meta[metaForkSources]; ok {
			t.Fatal("private source selection reached model metadata")
		}
		if len(req.Definition.Spec.Sources) != 2 {
			t.Fatal("changed pinned agent revision")
		}
	}
}
