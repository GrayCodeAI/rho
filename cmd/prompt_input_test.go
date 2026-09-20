package cmd

import (
	"bufio"
	"strings"
	"testing"

	"github.com/GrayCodeAI/rho/internal/contracts/policy"
	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/engine/safety"
	"github.com/GrayCodeAI/rho/internal/permissions"
	"github.com/GrayCodeAI/rho/internal/permissions/stableid"
)

func TestConfigureInteractivePromptsSessionApprovalIsScoped(t *testing.T) {
	sess := engine.NewSession("test", "test-model", "system", nil)
	configureInteractivePrompts(sess, promptInput{reader: bufio.NewReader(strings.NewReader("s\n")), close: func() {}})

	req := safety.PermissionRequest{
		PermissionRequest: policy.PermissionRequest{ToolName: "Bash", Summary: "git status"},
		Response:          make(chan bool, 1),
	}
	sess.PermSvc().PermissionFn()(req)
	if allowed := <-req.Response; !allowed {
		t.Fatal("session approval should allow the current request")
	}
	if decision := sess.PermSvc().Memory().Check("Bash", "git status"); decision == nil || !*decision {
		t.Fatal("session approval should remember the exact current action")
	}
	decision := sess.PermSvc().Memory().Check("Bash", "another command")
	if decision != nil {
		t.Fatalf("session approval widened to a different command: %v", *decision)
	}
}

func TestConfigureInteractivePromptsDefaultIsOneShot(t *testing.T) {
	sess := engine.NewSession("test", "test-model", "system", nil)
	configureInteractivePrompts(sess, promptInput{reader: bufio.NewReader(strings.NewReader("y\n")), close: func() {}})

	req := safety.PermissionRequest{
		PermissionRequest: policy.PermissionRequest{ToolName: "Bash", Summary: "git status"},
		Response:          make(chan bool, 1),
	}
	sess.PermSvc().PermissionFn()(req)
	if allowed := <-req.Response; !allowed {
		t.Fatal("one-shot approval should allow the current request")
	}
	if decision := sess.PermSvc().Memory().Check("Bash", "another command"); decision != nil {
		t.Fatalf("one-shot approval unexpectedly created a remembered rule: %v", *decision)
	}
}

func TestConfigureInteractivePromptsProjectRuleUsesExactIdentity(t *testing.T) {
	sess := engine.NewSession("test", "test-model", "system", nil)
	store := permissions.NewStableRuleStore(t.TempDir() + "/stable-rules.json")
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	sess.PermSvc().SetExactRuleStore(store)
	configureInteractivePrompts(sess, promptInput{reader: bufio.NewReader(strings.NewReader("p\n")), close: func() {}})

	identity := "git status --porcelain --branch"
	req := safety.PermissionRequest{
		PermissionRequest: policy.PermissionRequest{ToolName: "Bash", Summary: "git status...", Identity: identity},
		Response:          make(chan bool, 1),
	}
	sess.PermSvc().PermissionFn()(req)
	if allowed := <-req.Response; !allowed {
		t.Fatal("project approval should allow the current request")
	}
	rules := store.List()
	if len(rules) != 1 || rules[0].Decision != stableid.Allow || rules[0].DisplayIdentity != identity {
		t.Fatalf("unexpected exact project rule: %+v", rules)
	}
}

func TestConfigureInteractivePromptsSessionDenyIsScoped(t *testing.T) {
	sess := engine.NewSession("test", "test-model", "system", nil)
	configureInteractivePrompts(sess, promptInput{reader: bufio.NewReader(strings.NewReader("d\n")), close: func() {}})

	req := safety.PermissionRequest{
		PermissionRequest: policy.PermissionRequest{ToolName: "Write", Summary: "README.md"},
		Response:          make(chan bool, 1),
	}
	sess.PermSvc().PermissionFn()(req)
	if allowed := <-req.Response; allowed {
		t.Fatal("session denial should deny the current request")
	}
	decision := sess.PermSvc().Memory().Check("Write", "another file")
	if decision == nil || *decision {
		t.Fatal("session denial should remember a scoped deny rule")
	}
}
