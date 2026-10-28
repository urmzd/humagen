package slack_test

import (
	"encoding/json"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/fiber/pkg/address"
	"github.com/urmzd/humagen/fiber/pkg/event"
)

var target = address.MustParse("slack://T01ABCDEF/C02CHANNEL?thread=1700000000.000100")

func runEvent(seq uint64, kind string, payload string) event.Envelope {
	return event.Envelope{Seq: seq, Run: "run_1", Agent: "docs-bot", Type: kind, Data: wire.BodyOf(kind, []byte(payload)), Time: time.Unix(1700000100, 0).Add(time.Duration(seq) * time.Second)}
}

func textEvent(seq uint64, s string) event.Envelope {
	return runEvent(seq, event.TypeText, `{"text":`+jsonString(s)+`}`)
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
