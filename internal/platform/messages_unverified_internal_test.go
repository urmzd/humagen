package platform

import (
	"reflect"
	"testing"
)

// What a request's ref keeps from a caller no surface verified is where the
// place is, on the request's ref and on each message's.
func TestARequestsRefKeepsOnlyWhereThePlaceIs(t *testing.T) {
	said := ExternalRef{
		Type: RefSlack, Client: "app", Team: "T0TEAM", Channel: "G01SECRET", User: "U0ANA", TS: "1700000000.000300", ThreadTS: "1700000000.000200",
		Audience: &SurfaceAudience{Type: ChannelPrivate, Members: []string{"U0ANA"}}, AppUser: "UAPP", Files: true,
		Repo: "acme/web", Kind: "pull", Number: 5, CommentID: 9, Name: "nightly", Occurrence: "2026-01-01", URL: "https://example.com/page",
	}
	want := ExternalRef{
		Type: RefSlack, Team: "T0TEAM", Channel: "G01SECRET", TS: "1700000000.000300", ThreadTS: "1700000000.000200",
		Repo: "acme/web", Kind: "pull", Number: 5, CommentID: 9, Name: "nightly", Occurrence: "2026-01-01", URL: "https://example.com/page",
	}
	batch, own := said, said
	in := MessagesInput{Ref: &batch, Messages: []Message{{Role: "user", Ref: &own}, {Role: "user"}}}
	in.dropUnverified()
	if !reflect.DeepEqual(*in.Ref, want) {
		t.Errorf("the request's ref = %+v, want %+v", *in.Ref, want)
	}
	if !reflect.DeepEqual(*in.Messages[0].Ref, want) {
		t.Errorf("the message's ref = %+v, want %+v", *in.Messages[0].Ref, want)
	}
	if in.Messages[1].Ref != nil {
		t.Errorf("a message with no ref gained one: %+v", in.Messages[1].Ref)
	}
	if !reflect.DeepEqual(batch, said) || !reflect.DeepEqual(own, said) {
		t.Error("the caller's own refs were changed")
	}
	// Every field of a ref is one this test names, so a new one is decided here.
	if n := reflect.TypeOf(ExternalRef{}).NumField(); n != 17 {
		t.Errorf("a ref has %d fields, want 17: say here whether a caller no surface verified may set the new one", n)
	}
}
