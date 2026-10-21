package containerimage

import (
	"os/exec"
	"testing"
)

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
