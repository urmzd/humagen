package runs

import "testing"

func TestDeriveTitle(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"@code-reviewer Review the pull request urmzd/humagen#210", "Review the pull request urmzd/humagen#210"},
		{"@a @b  fix the build\nmore detail", "Fix the build"},
		{"@code-reviewer", "@code-reviewer"},
		{"   ", ""},
		{"draft the launch checklist:", "Draft the launch checklist"},
		{"summarize every incident from last quarter and group them by service owner please", "Summarize every incident from last quarter and group them…"},
	} {
		if got := DeriveTitle(c.in); got != c.want {
			t.Errorf("DeriveTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSessionTitle(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"@humagen-analyst How did the launch go?", "How did the launch go?"},
		{"howdy", "Howdy"},
		{"<@U123ABC> what shipped today", "What shipped today"},
		{"<@U123ABC|lead> <@U999> deploy it", "Deploy it"},
	} {
		if got := SessionTitle(c.in); got != c.want {
			t.Errorf("SessionTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGreeting(t *testing.T) {
	for in, want := range map[string]bool{
		"hi": true, "Hello!": true, "hey there": true, "howdy": true, "howdy mate": true,
		"yo": true, "good morning": true, "hiya :)": true, "sup": true, "hello @lead": true,
		"@humagen-analyst hi": true, "<@U123ABC> hey": true, "Hi 👋": true, "what's up": true,
		"hi, can you check my repos": false, "": false, "   ": false, "@lead": false,
		"mate": false, "there": false, "good": false, "check the build": false, "👋": false,
	} {
		if got := Greeting(in); got != want {
			t.Errorf("Greeting(%q) = %v, want %v", in, got, want)
		}
	}
}
