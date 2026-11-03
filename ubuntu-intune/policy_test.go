package containerimage

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestEdgeCommandLineFlagPolicy(t *testing.T) {
	var policy map[string]any
	if err := json.Unmarshal(EdgeCommandLineFlagPolicy, &policy); err != nil {
		t.Fatalf("parse Edge command-line flag policy: %v", err)
	}
	if len(policy) != 1 {
		t.Fatalf("policy has %d keys, want exactly 1: %v", len(policy), policy)
	}
	value, ok := policy["CommandLineFlagSecurityWarningsEnabled"]
	if !ok {
		t.Fatalf("policy missing CommandLineFlagSecurityWarningsEnabled: %v", policy)
	}
	if enabled, ok := value.(bool); !ok || enabled {
		t.Fatalf("CommandLineFlagSecurityWarningsEnabled = %v, want false", value)
	}
}

// The GPU process sandbox crashes inside nspawn, so the wrapper must keep this flag.
// EdgeCommandLineFlagPolicy, tested in TestEdgeCommandLineFlagPolicy, hides the Edge banner for it (issue #62).
// If someone removes the flag on purpose, they must also reconsider the policy.
func TestEdgeWrapperKeepsGPUSandboxFlag(t *testing.T) {
	wrapper, err := os.ReadFile("system_files/usr/local/bin/microsoft-edge")
	if err != nil {
		t.Fatalf("read Edge wrapper: %v", err)
	}

	const expected = `set -- '--disable-gpu-sandbox' "$@"`
	matches := 0
	for _, line := range strings.Split(string(wrapper), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == expected {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("Edge wrapper has %d GPU sandbox flag command lines, want exactly 1", matches)
	}
}

func TestBrokerRestartAuthorization(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("node is required to evaluate polkit regression tests")
	}
	// Execute the actual shipped JavaScript, with polkit's action/subject API.
	harness := `let rule; const polkit={Result:{YES:"yes"},addRule:r=>rule=r};
` + string(BrokerRestartPolicy) + `
for (const [id,unit,verb,member,want] of [
 ["org.freedesktop.systemd1.manage-units","microsoft-identity-device-broker.service","restart",true,"yes"],
 ["org.freedesktop.systemd1.manage-units","sshd.service","restart",true,undefined],
 ["org.freedesktop.systemd1.manage-units","microsoft-identity-device-broker.service","start",true,undefined],
 ["org.freedesktop.systemd1.manage-unit-files","microsoft-identity-device-broker.service","restart",true,undefined],
 ["org.freedesktop.systemd1.manage-units","microsoft-identity-device-broker.service","restart",false,undefined]
]) {
 const got=rule({id,lookup:k=>({unit,verb})[k]}, {isInGroup:g=>member&&g==="sudo"});
 if(got!==want) throw new Error(JSON.stringify({id,unit,verb,member,got,want}));
}
`
	if out, err := exec.Command(node, "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("policy: %v: %s", err, out)
	}
}
