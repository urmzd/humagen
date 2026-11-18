package sessionimport

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Branch is a line of the conversation off the main path: an edit or a
// retry a person moved on from, kept with the turn it forks after.
type Branch struct {
	// From is the original id of the turn the branch forks after. "" is a
	// branch that begins the conversation again from its start.
	From string
	// In is where that turn is: 0 on the main path, or the number of the
	// branch, counted from 1.
	In    int
	Turns []Turn
}

// MaxBranches bounds the branches kept of one conversation. Further ones
// are counted in Session.Branches.
const MaxBranches = 20

// maxID bounds an original id as a turn keeps it.
const maxID = 64

// ident is an original id as a turn keeps it: one word, cut to maxID.
func ident(s string) string { return clip(strings.Join(strings.Fields(s), ""), maxID) }

// node is one record of an export that says which record it follows.
type node struct {
	id, parent string
	turns      []Turn
}

// turns is what was said, in the order it is read.
type turns []Turn

// add appends a turn. Text one message said in pieces is joined: two pieces
// of the same role and message id, or two answers with no id and one time.
func (ts *turns) add(role, text string, at time.Time, id, call string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	id, call = ident(id), ident(call)
	if n := len(*ts); n > 0 && (role == "user" || role == "assistant") {
		last := &(*ts)[n-1]
		same := last.Role == role && last.ID == id && (id != "" || (role == "assistant" && last.At.Equal(at)))
		if same {
			last.Text += "\n\n" + text
			return
		}
	}
	*ts = append(*ts, Turn{Role: role, Text: text, At: at, ID: id, Call: call})
}

// attach names the files a message carried, on the turn that holds what the
// message said, or on a turn of its own when it said nothing.
func (ts *turns) attach(role string, files []attachment, at time.Time, id string) {
	text := attached(files)
	if text == "" {
		return
	}
	id = ident(id)
	for i := len(*ts) - 1; i >= 0 && id != ""; i-- {
		if t := &(*ts)[i]; t.Role == role && t.ID == id {
			t.Attached = text
			return
		}
	}
	*ts = append(*ts, Turn{Role: role, At: at, ID: id, Attached: text})
}

// said is whether a turn is something a person or the assistant wrote, and
// not a tool's call or result.
func said(t Turn) bool { return t.Role == "user" || t.Role == "assistant" }

// grow sets the session's turns from records that name their parents. The
// main path runs from the first record to leaf, each turn after the one it
// replies to, whatever their times say. A record off that path that holds
// something said starts a branch, kept with the id of the turn it forks
// after; one that holds only tool calls and results stays on the path,
// after the record it hangs from. Branches that fork after the same turn
// are in time order.
//
// chain says what a record with no parent among the records follows: the
// record before it (a file that goes on after its history was compacted),
// or nothing, which makes it a start of the conversation.
func (s *Session) grow(nodes []node, leaf int, chain bool) {
	s.Causal = true
	n := len(nodes)
	if n == 0 || leaf < 0 || leaf >= n {
		return
	}
	index := make(map[string]int, n)
	for i, nd := range nodes {
		if _, ok := index[nd.id]; nd.id != "" && !ok {
			index[nd.id] = i
		}
	}
	parent := make([]int, n)
	for i, nd := range nodes {
		p, ok := index[nd.parent]
		switch {
		case nd.parent != "" && ok && p != i:
			parent[i] = p
		case chain:
			parent[i] = i - 1
		default:
			parent[i] = -1
		}
	}
	// A record that is its own ancestor is cut loose, so every walk ends.
	state := make([]byte, n)
	for i := range nodes {
		var walk []int
		j, last := i, -1
		for j >= 0 && state[j] == 0 {
			state[j] = 1
			walk = append(walk, j)
			last, j = j, parent[j]
		}
		if j >= 0 && state[j] == 1 {
			parent[last] = -1
		}
		for _, k := range walk {
			state[k] = 2
		}
	}
	children := make([][]int, n)
	var order []int
	for i, p := range parent {
		if p < 0 {
			order = append(order, i)
		} else {
			children[p] = append(children[p], i)
		}
	}
	roots := len(order)
	for k := 0; k < len(order); k++ {
		order = append(order, children[order[k]]...)
	}
	// spoke is whether anything was said at or below a record, and tip is
	// the last record at or below it with a turn: where its branch ends.
	spoke, tip := make([]bool, n), make([]int, n)
	for i, nd := range nodes {
		tip[i] = -1
		if len(nd.turns) > 0 {
			tip[i] = i
		}
		for _, t := range nd.turns {
			spoke[i] = spoke[i] || said(t)
		}
	}
	for k := len(order) - 1; k >= 0; k-- {
		i := order[k]
		if p := parent[i]; p >= 0 {
			spoke[p] = spoke[p] || spoke[i]
			tip[p] = max(tip[p], tip[i])
		}
	}
	first := func(i int) time.Time {
		for stack := []int{i}; len(stack) > 0; {
			j := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, t := range nodes[j].turns {
				if !t.At.IsZero() {
					return t.At
				}
			}
			for k := len(children[j]) - 1; k >= 0; k-- {
				stack = append(stack, children[j][k])
			}
		}
		return time.Time{}
	}
	below := func(i int, owner []int, line int) []Turn {
		var out []Turn
		for stack := []int{i}; len(stack) > 0; {
			j := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			owner[j] = line
			out = append(out, nodes[j].turns...)
			for k := len(children[j]) - 1; k >= 0; k-- {
				stack = append(stack, children[j][k])
			}
		}
		return out
	}
	owner := make([]int, n)
	for i := range owner {
		owner[i] = -1
	}
	lines := 0
	var walk func(leaf, stop int, from string, in int)
	walk = func(leaf, stop int, from string, in int) {
		if lines > MaxBranches {
			s.Branches++
			return
		}
		line := lines
		lines++
		var path []int
		for j := leaf; j >= 0 && j != stop; j = parent[j] {
			path = append(path, j)
		}
		for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
			path[i], path[j] = path[j], path[i]
		}
		for _, j := range path {
			owner[j] = line
		}
		var ts []Turn
		var forks []int
		side := func(sibs []int, next int) {
			var apart []int
			for _, c := range sibs {
				switch {
				case c == next:
				case spoke[c]:
					apart = append(apart, c)
				default:
					ts = append(ts, below(c, owner, line)...)
				}
			}
			at := make([]time.Time, len(apart))
			for i, c := range apart {
				at[i] = first(c)
			}
			for _, i := range byTime(at) {
				forks = append(forks, apart[i])
			}
		}
		if line == 0 && len(path) > 0 {
			// Another start of the conversation forks before anything said.
			side(order[:roots], path[0])
		}
		for k, j := range path {
			ts = append(ts, nodes[j].turns...)
			next := -1
			if k+1 < len(path) {
				next = path[k+1]
			}
			side(children[j], next)
		}
		ts = afterCalls(ts)
		if line == 0 {
			s.Turns = ts
		} else {
			s.Side = append(s.Side, Branch{From: from, In: in, Turns: ts})
		}
		for _, c := range forks {
			// The turn a branch forks after is the last one said above it.
			from, in := "", 0
			for j := parent[c]; j >= 0; j = parent[j] {
				if k := len(nodes[j].turns); k > 0 {
					if from = nodes[j].turns[k-1].ID; from == "" {
						from = ident(nodes[j].id)
					}
					in = max(owner[j], 0)
					break
				}
			}
			walk(tip[c], parent[c], from, in)
		}
	}
	walk(leaf, -1, "", 0)
}

// byTime is the order of at by time: the indexes, earliest first. One with
// no time takes the time of the one before it, so it keeps its place after
// it, and equal times keep their order.
func byTime(at []time.Time) []int {
	eff := make([]time.Time, len(at))
	idx := make([]int, len(at))
	for i, t := range at {
		idx[i] = i
		if eff[i] = t; t.IsZero() && i > 0 {
			eff[i] = eff[i-1]
		}
	}
	sort.SliceStable(idx, func(a, b int) bool { return eff[idx[a]].Before(eff[idx[b]]) })
	return idx
}

// timed puts turns no record orders in time order, a tool's result still
// after its call.
func timed(ts []Turn) []Turn {
	at := make([]time.Time, len(ts))
	for i, t := range ts {
		at[i] = t.At
	}
	out := make([]Turn, 0, len(ts))
	for _, i := range byTime(at) {
		out = append(out, ts[i])
	}
	return afterCalls(out)
}

// afterCalls moves a tool's result that stands before its call to just
// after it: a result answers its call, whatever their times say.
func afterCalls(ts []Turn) []Turn {
	at := map[string]int{}
	for i, t := range ts {
		if t.Role != "tool" {
			continue
		}
		for _, id := range []string{t.Call, t.ID} {
			if _, ok := at[id]; id != "" && !ok {
				at[id] = i
			}
		}
	}
	early := map[int][]Turn{}
	moved := false
	keep := make([]bool, len(ts))
	for i, t := range ts {
		keep[i] = true
		if c, ok := at[t.Call]; ok && t.Role == "result" && t.Call != "" && c > i {
			early[c] = append(early[c], t)
			keep[i], moved = false, true
		}
	}
	if !moved {
		return ts
	}
	out := make([]Turn, 0, len(ts))
	for i, t := range ts {
		if keep[i] {
			out = append(out, t)
		}
		out = append(out, early[i]...)
	}
	return out
}

// attachment is a file a message carried, as the export describes it.
type attachment struct {
	name, kind string
	size       int64
	// text is what the export extracted from it.
	text  string
	image bool
}

// maxAttachments bounds the attachments named of one message.
const maxAttachments = 20

// String names the attachment with its type and size where the export
// states them, then its text. One with no text says what is not there.
func (a attachment) String() string {
	var parts []string
	for _, p := range []string{clip(oneLine(a.name), 120), clip(oneLine(a.kind), 80)} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if a.size > 0 {
		parts = append(parts, fmt.Sprintf("%d bytes", a.size))
	}
	head := "[attachment"
	if len(parts) > 0 {
		head += ": " + strings.Join(parts, ", ")
	}
	text := strings.TrimSpace(a.text)
	switch {
	case text != "":
		return head + "]\n" + result(text)
	case a.image:
		return head + "; the image itself is not imported]"
	}
	return head + "; its content is not in the export]"
}

// attached is a message's attachments as text, at most maxAttachments.
func attached(as []attachment) string {
	var out []string
	for i, a := range as {
		if i == maxAttachments {
			out = append(out, fmt.Sprintf("[and %d more attachments]", len(as)-i))
			break
		}
		out = append(out, a.String())
	}
	return strings.Join(out, "\n\n")
}

// isImage is whether a type or a file's name says it is an image.
func isImage(kind, name string) bool {
	if strings.Contains(strings.ToLower(kind), "image") {
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".heic", ".bmp", ".svg", ".tiff":
		return true
	}
	return false
}

// encoded is how many bytes base64 text holds.
func encoded(data string) int64 {
	data = strings.TrimRight(strings.TrimSpace(data), "=")
	return int64(base64.RawStdEncoding.DecodedLen(len(data)))
}

// dataURL reads the type and size out of a data: URL. Another URL has
// neither.
func dataURL(u string) (kind string, size int64) {
	rest, ok := strings.CutPrefix(u, "data:")
	if !ok {
		return "", 0
	}
	meta, data, ok := strings.Cut(rest, ",")
	if !ok {
		return "", 0
	}
	kind, _, _ = strings.Cut(meta, ";")
	if strings.HasSuffix(meta, ";base64") {
		return kind, encoded(data)
	}
	return kind, int64(len(data))
}
