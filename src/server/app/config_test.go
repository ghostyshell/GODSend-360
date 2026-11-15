package app

import "testing"

func TestMCPEnabledFromEnv(t *testing.T) {
	cases := map[string]bool{
		"":      true,
		"1":     true,
		"true":  true,
		"on":    true,
		"yes":   true,
		"junk":  true,
		"0":     false,
		"false": false,
		"FALSE": false,
		"off":   false,
		" No ":  false,
	}
	for value, want := range cases {
		t.Setenv("GODSEND_MCP_ENABLED", value)
		if got := MCPEnabledFromEnv(); got != want {
			t.Errorf("GODSEND_MCP_ENABLED=%q gave %v, want %v", value, got, want)
		}
	}
}
