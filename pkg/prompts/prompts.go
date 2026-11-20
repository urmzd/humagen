// Package prompts composes and pins the stable instruction prefix of a run.
package prompts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	MaxLayerChars      = 16000
	MaxPrefixChars     = 64000
	MaxLayerLabelChars = 256
	MaxSnapshotBytes   = MaxPrefixChars*6 + 16384
	DefaultSlackWords  = 120
	PlatformVersion    = "humagen.sessions.v3"
	MetaSnapshot       = "_humagen.prompts"
)

const Platform = `You are an agent in humagen. You work in a session: one conversation with you, started wherever a person is (Slack, GitHub, the console), answered there.
A message gets one answer. When a person mentions one agent, that agent answers the message where it was asked; when they mention several, each is asked first and the answer brings what they said together. @person asks a person. Bring in another agent with the agent tool when you have it: wait for its answer, or set background to hand the work over and carry on. Use session_read and session_link, when you have them, to read up on and cite other sessions. Raise an alert only when a person must act.
Lead with the answer. No preamble, no restating the question, no sign-off.
Match the place: in Slack, one to four sentences and no headings; in a GitHub review, the structured format; in the console, whatever fits.
Ask one question at a time, and only when you cannot proceed. Link to things instead of quoting them. React instead of acknowledging when a reaction tool is available. If you have nothing new to say, say nothing.`

var ErrInvalid = errors.New("prompts: invalid")

// Config overrides shipped instructions without changing code. Empty platform
// uses the shipped version. Zero SlackWords uses DefaultSlackWords.
type Config struct {
	Platform   string `json:"platform,omitempty"`
	Workspace  string `json:"workspace,omitempty"`
	SlackWords int    `json:"slack_word_budget,omitempty"`
}

func (c Config) Validate() error {
	for _, text := range []string{c.Platform, c.Workspace} {
		if !utf8.ValidString(text) || utf8.RuneCountInString(text) > MaxLayerChars || strings.ContainsRune(text, 0) {
			return fmt.Errorf("%w: shared instructions must be valid text of at most %d characters", ErrInvalid, MaxLayerChars)
		}
	}
	if c.SlackWords < 0 || c.SlackWords > 2000 {
		return fmt.Errorf("%w: Slack word budget must be from 1 to 2000, or zero for the default", ErrInvalid)
	}
	return nil
}

func (c Config) WordBudget() int {
	if c.SlackWords == 0 {
		return DefaultSlackWords
	}
	return c.SlackWords
}

// Layer records exactly the bytes used and their content-addressed version.
type Layer struct {
	Kind    string `json:"kind"`
	From    string `json:"from"`
	Text    string `json:"text"`
	Version string `json:"version"`
}

func NewLayer(kind, from, text string) Layer {
	text = strings.TrimSpace(text)
	h := sha256.Sum256([]byte(kind + "\x00" + from + "\x00" + text))
	return Layer{Kind: kind, From: from, Text: text, Version: hex.EncodeToString(h[:])}
}

// Snapshot is stored on the accepted run, never rebuilt on an attempt.
type Snapshot struct {
	Layers     []Layer `json:"layers"`
	SlackWords int     `json:"slack_word_budget"`
}

func Compose(c Config, projects []Layer) (Snapshot, error) {
	if err := c.Validate(); err != nil {
		return Snapshot{}, err
	}
	platform := c.Platform
	if platform == "" {
		platform = Platform
	}
	s := Snapshot{Layers: []Layer{NewLayer("platform", PlatformVersion, platform)}, SlackWords: c.WordBudget()}
	if c.Workspace != "" {
		s.Layers = append(s.Layers, NewLayer("workspace", "workspace", c.Workspace))
	}
	s.Layers = append(s.Layers, projects...)
	return s, s.Validate()
}

func (s Snapshot) Validate() error {
	if len(s.Layers) == 0 || len(s.Layers) > 64 || s.Layers[0].Kind != "platform" || s.SlackWords < 1 || s.SlackWords > 2000 {
		return fmt.Errorf("%w: malformed instruction snapshot", ErrInvalid)
	}
	total := 0
	for _, l := range s.Layers {
		for _, label := range []string{l.Kind, l.From} {
			if !utf8.ValidString(label) || utf8.RuneCountInString(label) > MaxLayerLabelChars || strings.ContainsRune(label, 0) {
				return fmt.Errorf("%w: instruction layer labels must be text of at most %d characters", ErrInvalid, MaxLayerLabelChars)
			}
		}
		n := utf8.RuneCountInString(l.Text)
		if !utf8.ValidString(l.Text) || strings.ContainsRune(l.Text, 0) || n > MaxLayerChars || l != NewLayer(l.Kind, l.From, l.Text) {
			return fmt.Errorf("%w: instruction layer failed its version check", ErrInvalid)
		}
		total += n
	}
	if total > MaxPrefixChars {
		return fmt.Errorf("%w: instruction prefix exceeds %d characters", ErrInvalid, MaxPrefixChars)
	}
	// JSON escaping and label metadata count toward the same limit Read uses.
	// Every accepted snapshot must survive its own persisted roundtrip.
	raw, err := json.Marshal(s)
	if err != nil || len(raw) > MaxSnapshotBytes {
		return fmt.Errorf("%w: encoded instruction snapshot exceeds %d bytes", ErrInvalid, MaxSnapshotBytes)
	}
	return nil
}

func (s Snapshot) Pin(meta map[string]string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("%w: no metadata", ErrInvalid)
	}
	b, err := json.Marshal(s)
	if err == nil {
		meta[MetaSnapshot] = string(b)
	}
	return err
}

func Read(meta map[string]string) (Snapshot, bool, error) {
	raw, ok := meta[MetaSnapshot]
	if !ok {
		return Snapshot{}, false, nil
	}
	if len(raw) > MaxSnapshotBytes {
		return Snapshot{}, true, ErrInvalid
	}
	var s Snapshot
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return s, true, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return s, true, s.Validate()
}

// System puts the exact pinned agent prompt after the shared stable prefix.
func (s Snapshot) System(agent string) string {
	texts := make([]string, 0, len(s.Layers)+1)
	for _, l := range s.Layers {
		if l.Text != "" {
			texts = append(texts, l.Text)
		}
	}
	return strings.Join(append(texts, strings.TrimSpace(agent)), "\n\n")
}
