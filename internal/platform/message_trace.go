package platform

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/journal"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/event"
)

// A message's trace is what ran to answer it, one node per run. For a
// person's message (or a session, read as its first message) it is the run
// that answered it, the response's root. For an agent's message it is that
// run. Beneath each run are the runs it called: its agent calls, the calls
// the platform made for the agents a message mentioned, and delegations
// alike. Each node says what its run did: the tools it called, the context
// it pulled with get_context, the sources it cited, and the approvals it
// waited on, each as the reader may see it. A run or child session the
// reader may not read is left out with everything beneath it, and counted
// in Hidden.

// Bounds on one trace.
const (
	// maxTraceNodes is how many runs one trace holds.
	maxTraceNodes = 500
	// maxTraceTools is how many tool calls, context reads, sources and
	// approvals one node holds, each.
	maxTraceTools = 200
	// maxTraceOutput is how much of a tool's output a node keeps, in bytes.
	maxTraceOutput = 4000
	// maxTraceSummary is how much of a context read's text a node keeps.
	maxTraceSummary = 1000
	// maxTraceSources is how many sources one context read lists.
	maxTraceSources = 20
)

// traceContextTool is the tool an agent pulls context from a message's
// place with. Its calls are a node's context reads, not its tools.
const traceContextTool = "get_context"

// Decisions on an approval, as a trace shows them.
const (
	TraceAllow = "allow"
	TraceDeny  = "deny"
	TraceAsk   = "ask"
)

// MessageTrace is a message's trace: the runs that answered it, as nodes,
// how many runs and child sessions were left out because the reader may not
// read them, and whether a bound cut it short.
type MessageTrace struct {
	Message   string      `json:"message"`
	Nodes     []TraceNode `json:"nodes"`
	Hidden    int         `json:"hidden"`
	Truncated bool        `json:"truncated"`
}

// TraceNode is one run: who ran, how it went, how long it took and what it
// spent (micro-USD), the model it used when that was recorded, its answer or
// its error, and what it did. Pick is set on a root the platform chose, and
// says why: the agents the message mentioned, or the search match that
// answered because the entry agent could not run.
type TraceNode struct {
	Run       string          `json:"run"`
	Agent     string          `json:"agent"`
	Status    string          `json:"status"`
	StartedAt time.Time       `json:"started_at"`
	EndedAt   time.Time       `json:"ended_at,omitzero"`
	Ms        int64           `json:"ms"`
	Cost      int64           `json:"cost,omitempty"`
	Pick      *TracePick      `json:"pick,omitempty"`
	Model     string          `json:"model,omitempty"`
	Answer    string          `json:"answer,omitempty"`
	Error     string          `json:"error,omitempty"`
	Tools     []TraceTool     `json:"tools"`
	Context   []TraceContext  `json:"context"`
	Sources   []TraceSource   `json:"sources"`
	Approvals []TraceApproval `json:"approvals"`
	Questions []TraceQuestion `json:"questions"`
	Children  []TraceNode     `json:"children"`
}

// TracePick is why a message's answer came from where it did, when the
// platform chose: the agents it names (those the reader may read), why, and
// the words that matched. It is activity, never a line of the conversation.
type TracePick struct {
	Decision string   `json:"decision"`
	Agents   []string `json:"agents"`
	Reason   string   `json:"reason"`
	Matched  []string `json:"matched"`
}

// TraceTool is one tool call: its id (htu_<run>_<seq>, as the response
// stream names it), when it was asked, how long it ran, and what it was
// given and gave back. Withheld says the reader may not see the input or
// the output, and both are left out.
type TraceTool struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	At       time.Time       `json:"at"`
	Ms       *int64          `json:"ms,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	Output   string          `json:"output,omitempty"`
	Error    string          `json:"error,omitempty"`
	Withheld bool            `json:"withheld"`
}

// TraceContext is one get_context call: its id and when it was asked (as a
// tool call's), how long it took, what the agent asked for, the place it
// read (bound by the platform to the message's own), the start of what came
// back, what that drew on, and whether it was cut.
type TraceContext struct {
	ID        string        `json:"id"`
	At        time.Time     `json:"at"`
	Ms        *int64        `json:"ms,omitempty"`
	Request   string        `json:"request"`
	Reference string        `json:"reference"`
	Summary   string        `json:"summary"`
	Sources   []TraceSource `json:"sources"`
	Truncated bool          `json:"truncated"`
	Error     string        `json:"error,omitempty"`
	Withheld  bool          `json:"withheld"`
}

// TraceSource is one thing a run cited or a context read drew on.
type TraceSource struct {
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
}

// TraceQuestion is a question a run asked a person with ask_person: a choice
// among options, or a reply in words, as the reader may see it. Tool is the
// id of the call that asked it, which POST /api/messages/{id}/answer takes.
// Status is asked while it waits, then answered, expired (nobody answered in
// time, so its default stood) or closed (its run ended first). Answer is the
// choice or the reply, and By who gave it, by name.
type TraceQuestion struct {
	ID      string    `json:"id"`
	Tool    string    `json:"tool,omitempty"`
	Kind    string    `json:"kind"`
	Prompt  string    `json:"prompt"`
	Options []string  `json:"options"`
	Status  string    `json:"status"`
	Answer  string    `json:"answer,omitempty"`
	By      string    `json:"by,omitempty"`
	At      time.Time `json:"at,omitzero"`
}

// How a question stands, as a trace shows it.
const (
	TraceAsked    = "asked"
	TraceAnswered = "answered"
	TraceExpired  = "expired"
	TraceClosed   = "closed"
)

// TraceApproval is an action a run waited on a person for, and how it was
// decided: allowed, denied, or still asked. By is who decided, by name.
// Tool is the id of the tool call it gates (a TraceTool's or a
// TraceContext's id), when it gates one; ID is the question's own.
type TraceApproval struct {
	ID       string    `json:"id"`
	Tool     string    `json:"tool,omitempty"`
	Action   string    `json:"action"`
	Decision string    `json:"decision"`
	By       string    `json:"by,omitempty"`
	Note     string    `json:"note,omitempty"`
	At       time.Time `json:"at,omitzero"`
}

// MessageTrace is the trace of the message id names: a session's id (its
// first message), a person's message (session:message), or an agent's
// (session:run).
func (p *Platform) MessageTrace(ctx context.Context, who auth.Principal, id string) (MessageTrace, error) {
	session, message := SplitMessageID(id)
	th, err := p.SessionRecord(ctx, who, session)
	if err != nil {
		return MessageTrace{}, err
	}
	t := &tracer{p: p, who: who, seer: p.Seer(who), names: p.shower(who.Tenant), root: th, seen: map[string]bool{}, trees: map[string][]runs.Run{}, picks: map[string]*TracePick{}, left: maxTraceNodes, now: p.now()}
	var nodes []TraceNode
	if message == "" {
		nodes = t.first()
	} else {
		message = strings.TrimPrefix(message, "pick-")
		if m, err := p.Threads.Message(who.Tenant, th.ID, message); err == nil {
			nodes = t.person(m)
		} else if r, err := p.Runs.Get(who.Tenant, message); err == nil && r.Thread == th.ID {
			nodes = t.agent(r)
		} else {
			return MessageTrace{}, threads.ErrNotFound
		}
	}
	return MessageTrace{Message: id, Nodes: nonNil(nodes), Hidden: t.hidden, Truncated: t.truncated}, nil
}

// tracer reads one trace for one reader: what it has worked out about each
// conversation and call tree, and how much room the trace has left.
type tracer struct {
	p     *Platform
	who   auth.Principal
	seer  *Seer
	names *shower
	root  threads.Thread
	seen  map[string]bool
	trees map[string][]runs.Run
	// picks is why the platform chose a root, by the root's run.
	picks map[string]*TracePick
	left  int
	now   time.Time

	hidden    int
	truncated bool
}

// first is the trace of a session's first message: a person's, else the
// run that started it.
func (t *tracer) first() []TraceNode {
	tenant := t.who.Tenant
	var m *threads.Message
	if msgs, _, err := t.p.Threads.Messages(tenant, t.root.ID, "", 1); err == nil && len(msgs) > 0 {
		m = &msgs[0]
	}
	var r *runs.Run
	if conv, err := t.p.Runs.Conversation(tenant, t.root.ID); err == nil && len(conv) > 0 {
		r = &conv[0]
	}
	switch {
	case m != nil && (r == nil || !m.At.After(r.CreatedAt)):
		return t.person(*m)
	case r != nil:
		return t.agent(*r)
	}
	return nil
}

// person is the trace of a person's message: the runs that answered it, in
// the order they started. The root the platform chose says why (Pick). A
// message from before answers were one run keeps its shape: the pick run
// that was made for it, with every answer it led to beneath it.
func (t *tracer) person(m threads.Message) []TraceNode {
	tenant := t.who.Tenant
	var answers []runs.Run
	if conv, err := t.p.Runs.Conversation(tenant, t.root.ID); err == nil {
		for _, r := range conv {
			if r.Message == m.ID {
				answers = append(answers, r)
			}
		}
	}
	var rec *pickRecord
	if it, err := t.p.picks.Lookup(tenant, pickKey(t.root.ID, m.ID)); err == nil {
		rec = &it.Value
		for _, c := range rec.Children {
			if !t.p.SeesSession(t.who, c.Session) {
				t.hidden++
				continue
			}
			if r, err := t.p.Runs.Get(tenant, c.Run); err == nil && c.Run != "" {
				answers = append(answers, r)
			}
		}
	}
	slices.SortStableFunc(answers, func(a, b runs.Run) int { return a.CreatedAt.Compare(b.CreatedAt) })
	if rec != nil && rec.Run != "" {
		pick := t.pick(rec.Pick)
		if slices.ContainsFunc(answers, func(r runs.Run) bool { return r.ID == rec.Run }) {
			// The root itself, chosen by the platform.
			t.picks[rec.Run] = &pick
			return t.each(answers)
		}
		// A pick run of its own, read wherever its root is: the root shows it.
		if pr, err := t.p.Runs.Get(tenant, rec.Run); err == nil {
			n, ok := t.tree(pr, &pick)
			if !ok {
				return nil
			}
			// Its answer was the pick itself.
			n.Answer = ""
			n.Children = append(n.Children, t.each(answers)...)
			return []TraceNode{n}
		}
	}
	return t.each(answers)
}

// agent is the trace of an agent's message: its run.
func (t *tracer) agent(r runs.Run) []TraceNode {
	return t.each([]runs.Run{r})
}

// each is the runs the reader may read, each with what is beneath it, while
// the trace has room.
func (t *tracer) each(list []runs.Run) []TraceNode {
	var out []TraceNode
	for _, r := range list {
		if !t.readable(r) {
			t.hidden++
			continue
		}
		n, ok := t.tree(r, t.picks[r.ID])
		if !ok {
			break
		}
		out = append(out, n)
	}
	return out
}

// tree is a run's node with the runs it called beneath it, or false when
// the trace has no room left for it.
func (t *tracer) tree(r runs.Run, pick *TracePick) (TraceNode, bool) {
	if t.left <= 0 {
		t.truncated = true
		return TraceNode{}, false
	}
	t.left--
	n := t.node(r, pick)
	n.Children = append(n.Children, t.each(t.called(r))...)
	return n, true
}

// called are the runs r called, oldest first.
func (t *tracer) called(r runs.Run) []runs.Run {
	tree, ok := t.trees[r.Root]
	if !ok {
		tree, _ = t.p.Runs.Tree(t.who.Tenant, r.ID)
		if len(tree) >= runs.MaxTree {
			t.truncated = true
		}
		t.trees[r.Root] = tree
	}
	var out []runs.Run
	for _, c := range tree {
		if c.Parent == r.ID && c.ID != r.ID {
			out = append(out, c)
		}
	}
	return out
}

// readable reports whether the reader may read a run: its agent, and the
// session it is in (or, outside any session, where it lives).
func (t *tracer) readable(r runs.Run) bool {
	if r.Tenant != t.who.Tenant || !t.seer.Agent(r.Agent) {
		return false
	}
	key := r.Project + "\x00" + r.Thread
	seen, ok := t.seen[key]
	if !ok {
		if th, err := t.p.Threads.Get(t.who.Tenant, r.Thread); err == nil && th.Session != nil {
			seen = t.p.SeesSession(t.who, r.Thread)
		} else {
			seen = t.seer.Run(r)
		}
		t.seen[key] = seen
	}
	return seen
}

// pick is a pick as the reader is shown it: only the agents they may read,
// and no reason when it would name one they may not.
func (t *tracer) pick(pk Pick) TracePick {
	out := TracePick{Decision: pk.Decision, Agents: []string{}, Reason: pk.Reason, Matched: nonNil(slices.Clone(pk.Matched))}
	for _, a := range pk.Agents {
		if t.seer.Agent(a) {
			out.Agents = append(out.Agents, a)
		}
	}
	if len(out.Agents) != len(pk.Agents) {
		out.Reason = ""
	}
	return out
}

// node is one run as the reader is shown it, without the runs beneath it.
func (t *tracer) node(r runs.Run, pick *TracePick) TraceNode {
	shown := t.p.ShownRunAs(t.who, r)
	n := TraceNode{Run: r.ID, Agent: r.Agent, Status: string(r.Status), StartedAt: r.CreatedAt.UTC(), Pick: pick, Cost: r.Spent,
		Tools: []TraceTool{}, Context: []TraceContext{}, Sources: []TraceSource{}, Approvals: []TraceApproval{}, Questions: []TraceQuestion{}, Children: []TraceNode{}}
	end := t.now
	if r.Status.Terminal() {
		n.EndedAt, end = r.UpdatedAt.UTC(), r.UpdatedAt
	}
	n.Ms = max(end.Sub(r.CreatedAt).Milliseconds(), 0)
	if r.Usage != nil && r.Usage.Context != nil {
		n.Model = r.Usage.Context.Model
	}
	n.Answer = shown.Answer
	if r.Status == runs.Failed {
		n.Error = shown.Error
	}
	for _, c := range shown.Citations {
		s := TraceSource{Title: c.Title, URL: c.URL}
		if s.Title == "" || slices.Contains(n.Sources, s) {
			continue
		}
		if len(n.Sources) == maxTraceTools {
			t.truncated = true
			break
		}
		n.Sources = append(n.Sources, s)
	}
	t.activity(&n, r)
	return n
}

// traceCall is one tool call as a run's events tell it.
type traceCall struct {
	id, name    string
	seq         int
	at          time.Time
	done        bool
	doneAt      time.Time
	input       json.RawMessage
	inputBytes  int
	output, err string
	withheld    bool
}

// toolData is a tool call's event data as the reader is shown it.
type toolData struct {
	Arguments      json.RawMessage `json:"arguments"`
	ArgumentsBytes int             `json:"arguments_bytes"`
	Result         string          `json:"result"`
	Error          string          `json:"error"`
}

// lifecycleData is a parked or resumed event's data as the reader is shown
// it.
type lifecycleData struct {
	Question *event.Question `json:"question"`
	Answer   *event.Answer   `json:"answer"`
	// A park on a batch of tool calls that each wait carries every
	// question, and its resume every answer.
	Questions []event.Question `json:"questions"`
	Answers   []event.Answer   `json:"answers"`
}

// questions is every question a parked event carries, in call order.
func (d lifecycleData) questions() []event.Question {
	return wire.QuestionsOf(event.Parked{Question: d.Question, Questions: d.Questions})
}

// answers is every answer a resumed event carries.
func (d lifecycleData) answers() []event.Answer {
	return wire.AnswersOf(event.Resumed{Answer: d.Answer, Answers: d.Answers})
}

// activity fills in what a run did, from its events as the run events route
// shows them to the reader: its tool calls, its context reads, its
// approvals, and the model of its latest call.
func (t *tracer) activity(n *TraceNode, r runs.Run) {
	tenant := t.who.Tenant
	held, err := t.p.Journal.Events(tenant, []string{r.ID})
	if err != nil {
		return
	}
	type spanKey struct {
		name string
		at   int64
	}
	spans := map[spanKey]journal.Span{}
	if list, err := t.p.Journal.Trace(tenant, []string{r.ID}); err == nil {
		for _, s := range list {
			if s.Kind == journal.SpanTool {
				spans[spanKey{s.Name, s.Start.UnixNano()}] = s
			}
		}
	}
	// A call's number counts every call the run asked for, whoever reads,
	// so its id is the same for every reader.
	seqs := callSeqs(held)
	calls := map[string]*traceCall{}
	var order []*traceCall
	call := func(id, name string, at time.Time) *traceCall {
		c, ok := calls[id]
		if !ok {
			c = &traceCall{id: id, name: name, seq: seqs[id], at: at}
			calls[id] = c
			order = append(order, c)
		}
		if c.name == "" {
			c.name = name
		}
		return c
	}
	approvals := map[string]*TraceApproval{}
	gates := map[*TraceApproval]string{}
	var asked []*TraceApproval
	// A question ask_person asked is at the latest ask_person call before
	// it (questionsAt), whoever reads.
	askedAt := map[string]string{}
	for call, ids := range questionsAt(held) {
		for _, id := range ids {
			askedAt[id] = call
		}
	}
	questions := map[string]*TraceQuestion{}
	var posed []*TraceQuestion
	for _, e := range held {
		if !t.p.CitationEventVisible(t.who, e) {
			continue
		}
		v, shown := t.p.ShownAPI(t.who, e)
		switch e.Type {
		case event.TypeToolCall, event.TypeToolDone:
			id, name := "", ""
			switch b := e.Data.(type) {
			case event.ToolCall:
				id, name = b.ID, b.Name
			case event.ToolDone:
				id, name = b.ID, b.Name
			}
			if id == "" {
				continue
			}
			c := call(id, name, e.Time)
			if e.Type == event.TypeToolDone {
				c.done, c.doneAt = true, e.Time
			}
			var d toolData
			if !shown || json.Unmarshal(v.Data, &d) != nil {
				c.withheld = true
				continue
			}
			if trimmed := strings.TrimSpace(string(d.Arguments)); strings.HasPrefix(trimmed, "{") {
				// The arguments a call ran with, which an approval may have
				// edited, come last.
				c.input = d.Arguments
			}
			c.inputBytes = max(c.inputBytes, d.ArgumentsBytes)
			if e.Type == event.TypeToolDone {
				c.output, c.err = d.Result, d.Error
			}
		case event.TypeParked:
			var d lifecycleData
			if !shown || json.Unmarshal(v.Data, &d) != nil {
				continue
			}
			// One park holds every question of a batch of waiting calls.
			for _, asks := range d.questions() {
				if asks.ID == "" {
					continue
				}
				if k := asks.Kind; k == event.QuestionChoice || k == event.QuestionReply {
					if _, ok := questions[asks.ID]; !ok {
						q := &TraceQuestion{ID: asks.ID, Kind: k, Prompt: asks.Prompt, Options: slices.Clone(asks.Options), Status: TraceAsked, At: asks.At}
						if q.Options == nil {
							q.Options = []string{}
						}
						if q.At.IsZero() {
							q.At = e.Time
						}
						if call, ok := askedAt[q.ID]; ok {
							q.Tool = toolUseID(r.ID, seqs[call])
						}
						questions[q.ID] = q
						posed = append(posed, q)
					}
					continue
				}
				if asks.Kind != event.QuestionApproval {
					continue
				}
				if _, ok := approvals[asks.ID]; ok {
					continue
				}
				a := &TraceApproval{ID: asks.ID, Action: headline(asks.Prompt, 200), Decision: TraceAsk, At: asks.At}
				if asks.Tool != nil && asks.Tool.Name != "" {
					a.Action = asks.Tool.Name
				}
				if asks.Tool != nil && asks.Tool.CallID != "" {
					gates[a] = asks.Tool.CallID
				}
				if a.At.IsZero() {
					a.At = e.Time
				}
				approvals[a.ID] = a
				asked = append(asked, a)
			}
		case event.TypeResumed:
			var d lifecycleData
			if !shown || json.Unmarshal(v.Data, &d) != nil {
				continue
			}
			for _, answer := range d.answers() {
				if a := approvals[answer.QuestionID]; a != nil && a.Decision == TraceAsk {
					t.decided(a, outcomeOf(answer, e.Time), answer.Decision)
				}
				if q := questions[answer.QuestionID]; q != nil && q.Status == TraceAsked {
					t.answered(q, outcomeOf(answer, e.Time), cmpOr(answer.Choice, answer.Text))
				}
			}
		case event.TypeUsageCall:
			if b, ok := e.Data.(event.UsageCall); ok && b.Model != "" {
				n.Model = b.Model
			}
		}
	}
	for _, c := range order {
		if c.input == nil && c.inputBytes > 0 {
			// Its size without the arguments themselves: they were kept back.
			c.withheld = true
		}
		if c.name == traceContextTool {
			if len(n.Context) == maxTraceTools {
				t.truncated = true
				continue
			}
			read := t.contextRead(c)
			read.ID, read.At, read.Ms = toolUseID(r.ID, c.seq), c.at.UTC(), callMs(c, spans[spanKey{c.name, c.at.UnixNano()}])
			n.Context = append(n.Context, read)
			continue
		}
		if len(n.Tools) == maxTraceTools {
			t.truncated = true
			continue
		}
		tool := TraceTool{ID: toolUseID(r.ID, c.seq), Name: c.name, At: c.at.UTC(), Ms: callMs(c, spans[spanKey{c.name, c.at.UnixNano()}]), Error: c.err, Withheld: c.withheld}
		if c.name == ToolSourceSearch && !c.withheld && !t.p.sourceOutputShown(t.who, r.ID, c.output) {
			// What a source search found is shown as its citations are.
			tool.Withheld = true
		}
		if c.name == ToolAgent && !c.withheld && !t.calleeShown(c.input) {
			// What an agent the reader may not read was asked, and what it
			// said, are kept as its run is.
			tool.Withheld = true
		}
		if !tool.Withheld {
			tool.Input = c.input
			if len(c.output) > maxTraceOutput {
				tool.Output = clipText(c.output, maxTraceOutput)
			} else {
				tool.Output = c.output
			}
		}
		n.Tools = append(n.Tools, tool)
	}
	for _, q := range posed {
		if q.Status == TraceAsked && r.Status.Terminal() {
			t.closed(q, r.ID)
		}
		if len(n.Questions) == maxTraceTools {
			t.truncated = true
			break
		}
		if !q.At.IsZero() {
			q.At = q.At.UTC()
		}
		n.Questions = append(n.Questions, *q)
	}
	for _, a := range asked {
		if a.Decision == TraceAsk && r.Status.Terminal() {
			t.settled(a, r.ID)
		}
		if len(n.Approvals) == maxTraceTools {
			t.truncated = true
			break
		}
		if !a.At.IsZero() {
			a.At = a.At.UTC()
		}
		if c, ok := calls[gates[a]]; ok {
			a.Tool = toolUseID(r.ID, c.seq)
		}
		n.Approvals = append(n.Approvals, *a)
	}
}

// calleeShown reports whether the reader may read the agent an agent call
// names. A call that names none is shown.
func (t *tracer) calleeShown(input json.RawMessage) bool {
	var in struct {
		Agent string `json:"agent"`
	}
	if input == nil || json.Unmarshal(input, &in) != nil || in.Agent == "" {
		return true
	}
	return t.seer.Agent(in.Agent)
}

// callMs is how long a call ran: its tool span when one was recorded, else
// from its start to its result, or nil while it runs.
func callMs(c *traceCall, s journal.Span) *int64 {
	switch {
	case !s.Start.IsZero() && !s.End.IsZero():
		ms := max(s.End.Sub(s.Start).Milliseconds(), 0)
		return &ms
	case c.done:
		ms := max(c.doneAt.Sub(c.at).Milliseconds(), 0)
		return &ms
	}
	return nil
}

// contextResult is what get_context answers.
type contextResult struct {
	Origin  string `json:"origin"`
	Text    string `json:"text"`
	Sources []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	} `json:"sources"`
	Truncated bool   `json:"truncated"`
	Note      string `json:"note"`
}

// contextRead is a get_context call as a context read.
func (t *tracer) contextRead(c *traceCall) TraceContext {
	out := TraceContext{ID: c.id, Sources: []TraceSource{}, Error: c.err, Withheld: c.withheld}
	if c.withheld {
		return out
	}
	var in struct {
		Request string `json:"request"`
	}
	if c.input != nil && json.Unmarshal(c.input, &in) == nil {
		out.Request = in.Request
	}
	var res contextResult
	if c.output != "" && json.Unmarshal([]byte(c.output), &res) == nil {
		out.Reference, out.Truncated = res.Origin, res.Truncated
		summary := res.Text
		if strings.TrimSpace(summary) == "" {
			summary = res.Note
		}
		if len(summary) > maxTraceSummary {
			summary = clipText(summary, maxTraceSummary)
		}
		out.Summary = summary
		for _, s := range res.Sources {
			title := cmpOr(s.Title, s.URL)
			if title == "" {
				continue
			}
			if len(out.Sources) == maxTraceSources {
				out.Truncated = true
				break
			}
			out.Sources = append(out.Sources, TraceSource{Title: title, URL: s.URL})
		}
	}
	return out
}

// decided records how an approval was settled. decision is the answer's,
// for one that expired and took its default.
func (t *tracer) decided(a *TraceApproval, o inbox.Outcome, decision string) {
	switch {
	case o.Action == ActApprove, o.Action == OutcomeExpired && decision == event.DecisionApprove:
		a.Decision = TraceAllow
	default:
		a.Decision = TraceDeny
	}
	a.Note, a.At = o.Note, o.At
	if o.By != "" && o.By != "system" {
		a.By = t.names.name(o.By)
	}
}

// answered records how a question was answered: the choice or the reply,
// who gave it, or that nobody did in time and its default stood.
func (t *tracer) answered(q *TraceQuestion, o inbox.Outcome, answer string) {
	q.Status, q.Answer, q.At = TraceAnswered, answer, o.At
	switch o.Action {
	case OutcomeExpired:
		q.Status = TraceExpired
	case OutcomeStopped:
		q.Status, q.Answer = TraceClosed, ""
		return
	}
	if q.Answer == "" && (o.Action == ActChoose || o.Action == ActReply) {
		q.Answer = o.Note
	}
	if o.By != "" && o.By != "system" {
		q.By = t.names.name(o.By)
	}
}

// closed records how a question its run ended without an answer to was
// settled: answered in its message, or closed when the run ended.
func (t *tracer) closed(q *TraceQuestion, run string) {
	q.Status = TraceClosed
	m, ok := t.p.Inbox.ByKey(t.who.Tenant, questionKey(run, q.ID))
	if !ok {
		return
	}
	for _, part := range m.Parts {
		if part.Question != nil && part.Outcome != nil {
			t.answered(q, *part.Outcome, "")
			return
		}
	}
}

// settled records how an approval its run ended without an answer to was
// settled: stopped, or closed when the run ended.
func (t *tracer) settled(a *TraceApproval, run string) {
	m, ok := t.p.Inbox.ByKey(t.who.Tenant, questionKey(run, a.ID))
	if !ok {
		return
	}
	for _, part := range m.Parts {
		if part.Question != nil && part.Outcome != nil {
			t.decided(a, *part.Outcome, "")
			return
		}
	}
}

// sourceOutputShown reports whether the reader may see what a source search
// found: only when they may see every citation it recorded, as a run's
// citations are shown (ShownRunAs).
func (p *Platform) sourceOutputShown(who auth.Principal, run, output string) bool {
	if !who.Can(auth.KnowledgeRead) {
		return false
	}
	if output == "" {
		return true
	}
	var found struct {
		Hits      []json.RawMessage `json:"hits"`
		Citations []runs.Citation   `json:"citations"`
	}
	if json.Unmarshal([]byte(output), &found) != nil || len(found.Citations) < len(found.Hits) {
		return false
	}
	for _, c := range found.Citations {
		proof, ok := p.SourceCitationEvidence(who.Tenant, run, c.ID, "")
		if !ok || !p.citationEvidenceVisible(who, proof) {
			return false
		}
	}
	return true
}
