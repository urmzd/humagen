package clients

import (
	"errors"
	"testing"
)

// A client with no home files its threads in the root's general channel, and
// a home is a project's name or ~ and a channel's slug, nothing else.
func TestAClientsHomeIsTheRootUnlessItNamesOne(t *testing.T) {
	c := slackOf("docs", "A0DOCS", "T01ABCDEF")
	if got := c.HomeOf(); got != (Home{Project: "", Channel: HomeChannel}) {
		t.Fatalf("no home = %+v", got)
	}
	for home, want := range map[Home]Home{
		{Project: "~"}:                       {Project: "", Channel: "general"},
		{Project: "Globex"}:                  {Project: "Globex", Channel: "general"},
		{Project: "Globex", Channel: "help"}: {Project: "Globex", Channel: "help"},
	} {
		c.Home = &home
		if err := c.Validate(); err != nil {
			t.Fatalf("%+v: %v", home, err)
		}
		if got := c.HomeOf(); got != want {
			t.Errorf("%+v = %+v, want %+v", home, got, want)
		}
	}
	for name, home := range map[string]Home{
		"no project":       {},
		"not a name":       {Project: "~/Globex"},
		"a channel's #":    {Project: "Globex", Channel: "#help"},
		"a capital":        {Project: "Globex", Channel: "Help"},
		"a long channel":   {Project: "Globex", Channel: "a123456789012345678901234567890123456789x"},
		"a path in a name": {Project: "Field/Globex"},
	} {
		c.Home = &home
		if err := c.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A copy's home is its own.
	c.Home = &Home{Project: "Globex"}
	cp := c.clone()
	cp.Home.Project = "Initech"
	if c.Home.Project != "Globex" {
		t.Fatal("a copy shares its home with the record")
	}
}
