package ai

// EventKind identifies the type of an engine Event.
type EventKind string

const (
	// EventThreadStarted carries the thread ID for later resumption.
	EventThreadStarted EventKind = "thread_started"
	// EventThinking is a reasoning summary from the model.
	EventThinking EventKind = "thinking"
	// EventCommandStarted reports a shell command the agent began running.
	EventCommandStarted EventKind = "command_started"
	// EventCommandCompleted reports a finished shell command with its exit code.
	EventCommandCompleted EventKind = "command_completed"
	// EventMessage is a complete agent message (markdown).
	EventMessage EventKind = "message"
	// EventActionProposal carries GitHub actions the agent proposes to perform.
	EventActionProposal EventKind = "action_proposal"
	// EventDone signals the turn completed successfully.
	EventDone EventKind = "done"
	// EventError signals the turn failed; Text holds the error message.
	EventError EventKind = "error"
)

// Event is a normalized engine event streamed to the UI while a turn runs.
type Event struct {
	Kind     EventKind
	ThreadID string   // set on EventThreadStarted
	Text     string   // message text, thinking summary, or error message
	Command  string   // shell command for command events
	ExitCode int      // exit code for EventCommandCompleted
	Actions  []Action // proposals for EventActionProposal
	Usage    *Usage   // set on EventDone
}

// Usage reports token consumption for a completed turn.
type Usage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
}
