package platform

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/attachment"
	filesblob "github.com/urmzd/saige/flow/blob"
	"github.com/urmzd/saige/flow/connector"
	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/speech"
)

// A Slack clip arrives on a run with no words: its turn hears it once, the
// agent reads the words, and the run is remembered as spoken.
func TestATurnHearsItsVoiceClipOnce(t *testing.T) {
	fake := &speech.Fake{Text: "Book the room for Friday.", Language: "english", Duration: 2 * time.Second}
	p, put := voicePlatform(t, fake)
	if err := p.settings.open(nil); err != nil {
		t.Fatal(err)
	}
	clip := put("audio_message.m4a", "audio/mp4", "....ftypM4A clip")
	ctx := context.Background()
	text, heard := p.hearTurn(ctx, "acme", "r1", "", []attachment.Attachment{clip})
	if !heard || !strings.Contains(text, "Book the room for Friday.") || !strings.HasPrefix(text, VoiceNoteMark) {
		t.Fatalf("heard %v: %q", heard, text)
	}
	again, heard := p.hearTurn(ctx, "acme", "r1", "", []attachment.Attachment{clip})
	if !heard || again != text {
		t.Fatalf("again %v: %q", heard, again)
	}
	if got, _ := fake.Heard(); len(got) != 1 {
		t.Fatalf("the clip was heard %d times", len(got))
	}
	if !p.runSpoken("acme", "r1") || p.runSpoken("acme", "r2") {
		t.Fatal("runSpoken")
	}
	// Words heard when the message was posted are not heard again.
	if same, heard := p.hearTurn(ctx, "acme", "r3", "[Voice note] hi", []attachment.Attachment{clip}); heard || same != "[Voice note] hi" {
		t.Fatalf("posted words heard again: %q", same)
	}
	if same, heard := p.hearTurn(ctx, "acme", "r4", "no audio", nil); heard || same != "no audio" {
		t.Fatal("no audio")
	}
}

// uploads is a Slack seat's peer that keeps what it is asked to upload.
type uploads struct {
	Peer
	mu   sync.Mutex
	sent []string
}

func (u *uploads) UploadClip(_ context.Context, channel, threadTS, name, title string, audio []byte) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sent = append(u.sent, strings.Join([]string{channel, threadTS, name, title, string(audio)}, "|"))
	return nil
}

func (u *uploads) Deliver(context.Context, address.Address, event.Envelope) error { return nil }
func (u *uploads) Ingest(context.Context, connector.Publish) error                { return nil }
func (u *uploads) ServeHTTP(http.ResponseWriter, *http.Request)                   {}

// A finished Slack run that answers a spoken clip has its answer spoken
// into the thread; a run that does not is left alone.
func TestASpokenSlackAnswerIsUploadedInItsThread(t *testing.T) {
	fake := &speech.Fake{Text: "hi", Duration: time.Second}
	p := &Platform{Runs: runs.NewIndex(), files: filesblob.NewMemory(), log: slog.New(slog.DiscardHandler), now: time.Now,
		cfg: Config{VoiceBuild: func(string, speech.Config) (Voice, error) { return fake, nil }}}
	if err := p.settings.open(nil); err != nil {
		t.Fatal(err)
	}
	if err := p.writeVoiceProvider("acme", &VoiceProvider{Kind: speech.KindGroq, Price: speech.GroqPrice, KeyStored: true}); err != nil {
		t.Fatal(err)
	}
	up := &uploads{}
	p.surfaces = map[string]*surface{"slack": {seats: []*seat{{client: clients.Client{Tenant: "acme", ID: "slack-1"}, peer: up}}}}
	finished := func(id, answer string) runs.Run {
		t.Helper()
		tag := wire.NewEvent(wire.Raise{ID: "ev-" + id, Tenant: "acme", Origin: runs.OriginSlack, Agent: "helper",
			Channel: "slack://T1/C1?thread=1700000000.000100", Context: map[string]string{runs.MetaClient: "slack-1"}, At: time.Now()})
		if err := p.Runs.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
		run, _ := p.Runs.Observe(event.Envelope{ID: "done-" + id, Type: event.TypeCompleted, Tenant: "acme", Run: id, Time: time.Now(), Data: event.Completed{Text: answer}})
		return run
	}
	quiet := finished("r-typed", "Typed answer.")
	if err := p.speakToSurface(context.Background(), quiet); err != nil || len(up.sent) != 0 {
		t.Fatalf("a typed message's answer was spoken: %v %v", err, up.sent)
	}
	spoken := finished("r-spoken", "The room is booked for Friday.")
	if spoken.Status != runs.Completed || spoken.Client != "slack-1" {
		t.Fatalf("run = %+v", spoken)
	}
	op, _ := recordOp("acme", voiceTurnKey("r-spoken"), voiceTurn{Text: "[Voice note] book it", Spoken: true}, 0)
	if err := p.voiceUsage().write(op); err != nil {
		t.Fatal(err)
	}
	if err := p.speakToSurface(context.Background(), spoken); err != nil {
		t.Fatal(err)
	}
	if len(up.sent) != 1 || up.sent[0] != "C1|1700000000.000100|answer.wav|Spoken answer|The room is booked for Friday." {
		t.Fatalf("uploaded %v", up.sent)
	}
}
