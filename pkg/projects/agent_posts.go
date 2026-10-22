package projects

// What becomes of a post an agent scheduled in a project writes for it.
const (
	// AgentPostsPublish posts it to the project at once, from the agent.
	AgentPostsPublish = "publish"
	// AgentPostsReview holds it as a draft until one of the project's owners
	// publishes or discards it.
	AgentPostsReview = "review"
)

func validAgentPosts(v string) bool {
	return v == "" || v == AgentPostsPublish || v == AgentPostsReview
}

// EffectiveAgentPosts is what a project does with an agent's post: its own
// setting, or else its kind's default. The root and company projects, which
// everyone reads, review; team and account projects publish. A name the tree
// does not hold reviews: what is not known is held for a person.
func EffectiveAgentPosts(t *Tree, name string) string {
	p, ok := t.Get(name)
	switch {
	case !ok:
		return AgentPostsReview
	case p.AgentPosts != "":
		return p.AgentPosts
	case p.IsRoot() || p.Kind == Company:
		return AgentPostsReview
	}
	return AgentPostsPublish
}
