package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/anchor"
	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/turn"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
)

// prepareCall resolves references once, before accepting the child. The tag
// stores the resolved bytes as well as their provenance, so retries never read
// newer conversation turns, changed artifacts, or a different result.
func (p *Platform) prepareCall(ctx context.Context, tenant string, input call.Input) (call.Prepared, error) {
	if err := input.Validate(); err != nil {
		return call.Prepared{}, err
	}
	out := call.Prepared{Input: input}
	size := 0
	for _, m := range input.Messages {
		var parts []string
		for _, part := range m.Content {
			text, err := p.resolveCallPart(ctx, tenant, part)
			if err != nil {
				return call.Prepared{}, err
			}
			size += len(text) + 2
			if size > call.MaxInputBytes {
				return call.Prepared{}, fmt.Errorf("%w: resolved input exceeds %d bytes", call.ErrInvalid, call.MaxInputBytes)
			}
			parts = append(parts, text)
		}
		out.Messages = append(out.Messages, call.ResolvedMessage{Role: m.Role, Text: strings.Join(parts, "\n\n")})
	}
	return out, out.Validate()
}
func (p *Platform) resolveCallPart(ctx context.Context, tenant string, part call.Part) (string, error) {
	switch v := part.(type) {
	case call.TextPart:
		return v.Text, nil
	case call.RunResultPart:
		r, err := p.Runs.Get(tenant, v.Run)
		if err != nil {
			return "", err
		}
		if r.Status != runs.Completed {
			return "", fmt.Errorf("%w: referenced result is not completed", call.ErrInvalid)
		}
		return r.Answer, nil
	case call.ConversationRange:
		var selected []runs.Run
		id := v.Through
		for len(selected) < call.MaxReferenceTurns {
			r, err := p.Runs.Get(tenant, id)
			if err != nil {
				return "", err
			}
			if r.Thread != v.Thread || r.Parent != "" {
				return "", fmt.Errorf("%w: range is outside the named conversation branch", call.ErrInvalid)
			}
			if !r.Status.Terminal() {
				return "", fmt.Errorf("%w: referenced conversation turn has not ended", call.ErrInvalid)
			}
			selected = append(selected, r)
			if id == v.From {
				slices.Reverse(selected)
				var b strings.Builder
				for _, turn := range selected {
					fmt.Fprintf(&b, "User: %s\nAssistant (%s, %s): %s\n", turn.Text, turn.Agent, turn.Status, turn.Answer)
					if b.Len() > call.MaxInputBytes {
						return "", fmt.Errorf("%w: conversation range is too large", call.ErrInvalid)
					}
				}
				return b.String(), nil
			}
			id = r.Prev
			if id == "" {
				break
			}
		}
		return "", fmt.Errorf("%w: range endpoints must share a branch of at most %d turns", call.ErrInvalid, call.MaxReferenceTurns)
	case call.ArtifactPart:
		a, err := p.Artifacts.Get(tenant, v.ID)
		if err != nil {
			return "", err
		}
		if a.Version != v.Version {
			return "", fmt.Errorf("%w: artifact version does not match", call.ErrInvalid)
		}
		if a.Size > call.MaxInputBytes {
			return "", fmt.Errorf("%w: artifact is too large", call.ErrInvalid)
		}
		r, _, err := p.Blobs.Get(ctx, tenant, a.Key)
		if err != nil {
			return "", err
		}
		defer r.Close()
		b, err := io.ReadAll(io.LimitReader(r, call.MaxInputBytes+1))
		if err != nil {
			return "", err
		}
		if len(b) > call.MaxInputBytes || !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
			return "", fmt.Errorf("%w: artifact must be bounded UTF-8 text", call.ErrInvalid)
		}
		return string(b), nil
	default:
		return "", fmt.Errorf("%w: unsupported input part", call.ErrInvalid)
	}
}

// callExecutor adapts explicit helper messages to the turn's history and
// request. It composes the sandbox; it never owns an agent loop.
type callExecutor struct {
	bind  func(context.Context, runtime.Request) func()
	inner turn.Runtime
	// resolve puts in place what the platform decides about the pinned
	// definition before the turn reads it: a workspace's model policy for
	// the one an agent names, the knowledge sources the run's project chain
	// was pinned with, and the skills the run was told to use
	// (resolveRevision). Nil leaves the definition as it is.
	resolve func(runtime.Request) (runtime.Request, error)
	release func(runtime.Request)
}

var _ turn.Runtime = callExecutor{}

// Turn runs one turn of the runtime's request through the sandbox. The
// request travels on ctx (runtime.RequestFrom); what this changes is handed
// on with runtime.WithRequest, and mirrored on in for a runtime that reads
// only its arguments.
func (e callExecutor) Turn(ctx context.Context, def turn.Pinned, in turn.Input, emit func(context.Context, event.Body) error) (turn.Result, error) {
	req, ok := runtime.RequestFrom(ctx)
	if !ok {
		req = runtime.Request{Definition: def, Tenant: in.Tenant, RunID: in.Trigger.Run, Text: wire.TextOf(in.Trigger), Meta: wire.Meta(in.Trigger), Origin: wire.Origin(in.Trigger)}
	}
	defer func() {
		if e.release != nil {
			e.release(req)
		}
	}()
	if e.resolve != nil {
		var err error
		if req, err = e.resolve(req); err != nil {
			return turn.Result{}, err
		}
	}
	if e.bind != nil {
		done := e.bind(ctx, req)
		defer done()
	}
	var format call.ResponseFormat
	if req.Origin == call.Origin {
		raw := req.Meta[call.MetaInput]
		if raw != "" {
			var prepared call.Prepared
			if err := call.StrictDecode([]byte(raw), &prepared); err != nil {
				return turn.Result{}, err
			}
			if err := prepared.Validate(); err != nil {
				return turn.Result{}, err
			}
			edge, err := call.EdgeOf(in.Trigger)
			if err != nil {
				return turn.Result{}, err
			}
			format = prepared.Input.ResponseFormat
			req.Instructions = format.Instruction()
			if edge.Mode != call.Handoff {
				req.Instructions += "\nYou are a helper. Return your result to the calling agent, which owns the user-facing answer. Do not present a second user-facing answer unless asked to draft text. Background results are recorded for later inspection."
			}
			req.History = nil
			for _, m := range prepared.Messages[:len(prepared.Messages)-1] {
				req.History = append(req.History, native.Message{Role: native.Role(m.Role), Text: m.Text})
			}
			req.Text = prepared.Messages[len(prepared.Messages)-1].Text
			// Provenance belongs in the journal, not in the model's prompt.
			req.Meta = nil
			in.Trigger = wire.Clone(in.Trigger)
			wire.SetText(&in.Trigger, req.Text)
			wire.SetContext(&in.Trigger, nil)
			in.History = req.History
		}
	}
	if len(req.Definition.Spec.Access.Spawn) > 0 {
		req.Instructions += "\nYou own the answer to your caller. Use call_agent for bounded helper work with explicit input messages and a return format. Incorporate useful results into one answer; do not narrate and repeat each helper's full answer."
	}
	in.Instructions = req.Instructions
	// The run's history budget (compaction_tokens) was resolved onto req;
	// a configured runtime reads only in.
	in.Compaction = native.CompactionTo(req.Compaction)
	// Buffer contracted outputs until validation succeeds. An invalid helper
	// answer must not appear as accepted text or a successful tool result.
	var buffered []event.Body
	send := emit
	if format != nil {
		size := 0
		send = func(ctx context.Context, b event.Body) error {
			if t, ok := b.(event.Text); ok {
				size += len(t.Text)
				if size > call.MaxOutputChars*6 {
					return fmt.Errorf("%w: helper output exceeds stream limit", call.ErrInvalid)
				}
				buffered = append(buffered, b)
				return nil
			}
			if emit != nil {
				return emit(ctx, b)
			}
			return nil
		}
	}
	ctx = runtime.WithRequest(ctx, req)
	ctx = context.WithValue(ctx, operationRequestKey{}, req)
	out, err := e.inner.Turn(ctx, req.Definition, in, send)
	if err != nil {
		return out, err
	}
	if format != nil {
		if utf8.RuneCountInString(out.Text) > call.MaxOutputChars {
			return turn.Result{}, anchor.NonRetryable(fmt.Errorf("%w: helper output exceeds limit", call.ErrInvalid))
		}
		if err := format.ValidateOutput(out.Text); err != nil {
			return turn.Result{}, anchor.NonRetryable(err)
		}
		for _, b := range buffered {
			if emit != nil {
				if err := emit(ctx, b); err != nil {
					return turn.Result{}, err
				}
			}
		}
	}
	return out, nil
}

func freezeInput(tagMeta map[string]string, prepared call.Prepared) error {
	b, err := json.Marshal(prepared)
	if err != nil {
		return err
	}
	tagMeta[call.MetaInput] = string(b)
	return nil
}

func sameCallInput(existing runs.Run, req call.Request, input call.Input) bool {
	if existing.Agent != req.Agent || existing.Mode != req.Mode || existing.Lease != req.Lease {
		return false
	}
	raw := wire.Meta(existing.Tag())[call.MetaInput]
	if raw == "" {
		return req.Input == nil && existing.Text == req.Text
	}
	var frozen call.Prepared
	if err := call.StrictDecode([]byte(raw), &frozen); err != nil {
		return false
	}
	a, err := json.Marshal(input)
	if err != nil {
		return false
	}
	b, err := json.Marshal(frozen.Input)
	return err == nil && string(a) == string(b)
}
