package chat

// TurnState tracks semantic facts about the current assistant turn. It is
// deliberately independent of rendering, persistence, and Bubble Tea state.
type TurnState struct {
	sawThinking        bool
	hadAssistantOutput bool
	hadToolActivity    bool
}

func (s *TurnState) Reset() {
	s.sawThinking = false
	s.hadAssistantOutput = false
	s.hadToolActivity = false
}

func (s *TurnState) ObserveThinking() { s.sawThinking = true }
func (s *TurnState) ObserveContent()  { s.hadAssistantOutput = true }
func (s *TurnState) ObserveTool()     { s.hadToolActivity = true }

func (s TurnState) SawThinking() bool        { return s.sawThinking }
func (s TurnState) HadAssistantOutput() bool { return s.hadAssistantOutput }
func (s TurnState) HadToolActivity() bool    { return s.hadToolActivity }

// ReasoningOnly reports the provider failure mode where hidden reasoning was
// received but neither an answer nor a tool action was produced.
func (s TurnState) ReasoningOnly() bool {
	return s.sawThinking && !s.hadAssistantOutput && !s.hadToolActivity
}
