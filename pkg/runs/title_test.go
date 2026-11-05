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
