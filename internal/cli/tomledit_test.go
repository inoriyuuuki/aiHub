package cli

import (
	"strings"
	"testing"
)

func TestMergeManagedTOMLPreservesOthers(t *testing.T) {
	existing := `# user config
[mcp_servers.other]
command = "other"

[model]
provider = "openai"
`
	managed := map[string]string{
		"aihub-prof-demo": "command = \"npx\"\nargs = [\"-y\", \"demo\"]\nenv = { KEY = \"v\" }\n",
	}
	out, err := mergeManagedTOML([]byte(existing), managed)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "[mcp_servers.other]") {
		t.Fatal("unmanaged section must be preserved")
	}
	if !strings.Contains(s, "provider = \"openai\"") {
		t.Fatal("unmanaged content must be preserved")
	}
	if !strings.Contains(s, "[mcp_servers.aihub-prof-demo]") {
		t.Fatal("managed section must be appended")
	}
	if !strings.Contains(s, "command = \"npx\"") {
		t.Fatal("managed body missing")
	}
}

func TestMergeManagedTOMLRemovesOldManaged(t *testing.T) {
	existing := `[mcp_servers.aihub-prof-a]
command = "old"

[other]
x = 1
`
	managed := map[string]string{"aihub-prof-a": "command = \"new\"\n"}
	out, err := mergeManagedTOML([]byte(existing), managed)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "command = \"old\"") {
		t.Fatal("old managed content should be replaced")
	}
	if !strings.Contains(s, "command = \"new\"") {
		t.Fatal("new managed content missing")
	}
	if !strings.Contains(s, "[other]") {
		t.Fatal("other section must be preserved")
	}
}

func TestMergeManagedTOMLConflict(t *testing.T) {
	existing := `[mcp_servers.aihub-prof-demo]
command = "other"
`
	// The name is not in our managed set -> refuse.
	managed := map[string]string{"aihub-other": "command = \"x\"\n"}
	if _, err := mergeManagedTOML([]byte(existing), managed); err == nil {
		t.Fatal("expected conflict error")
	}
}
