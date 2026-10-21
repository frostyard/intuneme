package provision

import (
	"os/exec"
	"testing"
)

func TestHostShellAuthorizationDecisions(t *testing.T) {
	rule, err := polkitRule("intuneme", "alice", 1000)
	if err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("node required for polkit decision tests")
	}
	harness := `let rule; const polkit={Result:{YES:"yes"},addRule:r=>rule=r};
` + rule + `
for (const [id,machine,user,uid,want] of [
 ["shell","intuneme","alice",1000,"yes"],
 ["shell","intuneme","root",1000,undefined],
 ["shell","intuneme","0",1000,undefined],
 ["shell","intuneme","1000",1000,undefined],
 ["shell","intuneme","bob",1000,undefined],
 ["shell","other","alice",1000,undefined],
 ["shell",".host","alice",1000,undefined],
 ["shell","intuneme","alice",1001,undefined],
 ["host-shell",".host","alice",1000,undefined],
 ["manage-machines","intuneme","alice",1000,undefined],
 ["shell",undefined,undefined,1000,undefined]
]) {
 const got=rule({id:"org.freedesktop.machine1."+id,lookup:k=>({machine,user})[k]}, {uid,isInGroup:()=>true});
 if(got!==want)throw new Error(JSON.stringify({id,machine,user,uid,got,want}));
}
`
	if out, err := exec.Command(node, "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("policy: %v: %s", err, out)
	}
	for _, user := range []string{"root", "0", "00", "1000"} {
		if _, err := polkitRule("intuneme", user, 1000); err == nil {
			t.Fatal("accepted root identity")
		}
	}
}
