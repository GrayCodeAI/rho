package config

import "testing"

func TestParseCommand(t *testing.T) {
	tests := []struct {
		parts  []string
		action Action
		key    string
		value  string
	}{
		{[]string{"/config"}, ActionOpen, "", ""},
		{[]string{"/config", "provider", "open", "ai"}, ActionProvider, "", "open ai"},
		{[]string{"/config", "model", "model-x"}, ActionModel, "", "model-x"},
		{[]string{"/config", "keys"}, ActionKeys, "", ""},
		{[]string{"/config", "key", "remove"}, ActionRemoveKey, "", ""},
		{[]string{"/config", "get", "theme"}, ActionGet, "theme", ""},
		{[]string{"/config", "set", "theme", "dark"}, ActionSet, "theme", "dark"},
	}
	for _, tt := range tests {
		got, err := ParseCommand(tt.parts)
		if err != nil || got.Action != tt.action || got.Key != tt.key || got.Value != tt.value {
			t.Errorf("ParseCommand(%v) = %#v, %v", tt.parts, got, err)
		}
	}
}

func TestParseCommandRejectsExtraRemoveKeyArgument(t *testing.T) {
	if _, err := ParseCommand([]string{"/config", "key", "remove", "extra"}); err == nil {
		t.Fatal("expected usage error")
	}
}

func TestParseCommandRejectsUnknownAction(t *testing.T) {
	if _, err := ParseCommand([]string{"/config", "wat"}); err == nil {
		t.Fatal("expected unknown config action to return a usage error")
	}
}

func TestParseCommandRejectsIncompleteGetAndSet(t *testing.T) {
	for _, parts := range [][]string{
		{"/config", "get"},
		{"/config", "set", "theme"},
		{"/config", "keys", "extra"},
		{"/config", "key", "delete"},
	} {
		if _, err := ParseCommand(parts); err == nil {
			t.Errorf("ParseCommand(%v) returned nil error", parts)
		}
	}
}

func TestNormalizeKey(t *testing.T) {
	if got := NormalizeKey(" Reasoning-Effort "); got != "reasoningeffort" {
		t.Fatalf("NormalizeKey = %q", got)
	}
}
