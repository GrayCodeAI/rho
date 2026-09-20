package cmd

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type unexpectedFinalChatModel struct{}

func (unexpectedFinalChatModel) Init() tea.Cmd { return nil }

func (m unexpectedFinalChatModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (unexpectedFinalChatModel) View() tea.View { return tea.View{} }

func TestFinalChatModelAcceptsValueAndPointer(t *testing.T) {
	want := chatModel{sessionID: "session-1", quitting: true}

	for name, input := range map[string]tea.Model{
		"value":   want,
		"pointer": &want,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := finalChatModel(input)
			if err != nil {
				t.Fatalf("finalChatModel() error = %v", err)
			}
			if got.sessionID != want.sessionID || got.quitting != want.quitting {
				t.Fatalf("finalChatModel() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestFinalChatModelRejectsUnexpectedModel(t *testing.T) {
	_, err := finalChatModel(unexpectedFinalChatModel{})
	if err == nil || !strings.Contains(err.Error(), "unexpected final model type") {
		t.Fatalf("finalChatModel() error = %v, want unexpected model error", err)
	}
}
