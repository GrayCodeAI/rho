package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/GrayCodeAI/rho/internal/engine"
	"github.com/GrayCodeAI/rho/internal/engine/safety"
	"github.com/GrayCodeAI/rho/internal/permissions/stableid"
)

var errNoInteractivePromptInput = errors.New("no interactive terminal available for permission prompt")

type promptInput struct {
	reader *bufio.Reader
	close  func()
}

func openPromptInput() promptInput {
	if isStdinTerminal() {
		return promptInput{
			reader: bufio.NewReader(os.Stdin),
			close:  func() {},
		}
	}

	tty, err := os.Open("/dev/tty")
	if err != nil {
		return promptInput{close: func() {}}
	}
	return promptInput{
		reader: bufio.NewReader(tty),
		close: func() {
			_ = tty.Close()
		},
	}
}

func (p promptInput) readLine(prompt string) (string, error) {
	if p.reader == nil {
		return "", errNoInteractivePromptInput
	}
	_, _ = fmt.Fprint(os.Stderr, prompt)
	answer, err := p.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(answer), nil
}

func configureInteractivePrompts(sess *engine.Session, input promptInput) {
	sess.PermSvc().SetPermissionFn(func(req safety.PermissionRequest) {
		answer, err := input.readLine(fmt.Sprintf("\nAllow %s: %s [y/N, s=session, d=deny session, p=allow project, x=deny project] ", req.ToolName, req.Summary))
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "\nAllow %s: %s (denied: %v)\n", req.ToolName, req.Summary, err)
			resolvePermissionResponse(&req, false)
			return
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		allow := answer == "y" || answer == "yes"
		var remember func()
		switch answer {
		case "s", "session":
			allow = true
			remember = func() { _ = rememberSessionPermission(sess, &req, true) }
		case "d", "deny-session":
			allow = false
			remember = func() { _ = sess.PermSvc().RememberSessionTool(req.ToolName, false) }
		case "p", "allow-project", "x", "deny-project":
			decision := stableid.Allow
			if answer == "x" || answer == "deny-project" {
				decision = stableid.Deny
			}
			allow = sess.PermSvc().ExactRulesConfigured()
			if decision == stableid.Deny {
				allow = false
			}
			if allow {
				remember = func() { _, _ = rememberExactPermissionForSession(sess, &req, decision) }
			}
		}
		if !resolvePermissionResponse(&req, allow) {
			// A cancelled or stale request must not leave behind a remembered rule.
			return
		}
		if remember != nil {
			remember()
		}
	})
	sess.SetAskUserFn(func(question string) (string, error) {
		return input.readLine(fmt.Sprintf("\n%s\n> ", question))
	})
}
