package cache

import "testing"

func TestIsAccessRestricted(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{`"true"`, true},
		{`"TRUE"`, true},
		{`"false"`, false},
		{`"logged-in"`, false},
		{`true`, true},   // bare JSON bool
		{`false`, false}, // bare JSON bool
		{`null`, false},
		{``, false},
		{`["true"]`, true},
		{`["false", "true"]`, true},
		{`["logged-in"]`, false},
		{`[]`, false},
	}
	for _, c := range cases {
		var raw []byte
		if c.in != "" {
			raw = []byte(c.in)
		}
		if got := isAccessRestricted(raw); got != c.want {
			t.Errorf("isAccessRestricted(%s) = %v, want %v", c.in, got, c.want)
		}
	}
}