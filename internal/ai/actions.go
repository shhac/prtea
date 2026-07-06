package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ActionType identifies a GitHub action the agent can propose.
type ActionType string

const (
	ActionPostComment    ActionType = "post_comment"
	ActionReplyToComment ActionType = "reply_to_comment"
	ActionSubmitReview   ActionType = "submit_review"
)

// Action is a GitHub operation proposed by the agent. prtea shows it to the
// user for confirmation and executes it through the GitHub client — the agent
// itself has no network access.
type Action struct {
	Type      ActionType `json:"type"`
	Body      string     `json:"body"`
	CommentID int64      `json:"comment_id,omitempty"` // reply_to_comment
	Event     string     `json:"event,omitempty"`      // submit_review: approve|comment|request_changes
}

// Describe returns a short human-readable summary for the confirmation prompt.
func (a Action) Describe() string {
	switch a.Type {
	case ActionPostComment:
		return "Post PR comment"
	case ActionReplyToComment:
		return fmt.Sprintf("Reply to comment %d", a.CommentID)
	case ActionSubmitReview:
		return fmt.Sprintf("Submit review (%s)", a.Event)
	default:
		return string(a.Type)
	}
}

// Validate checks that the action has the fields its type requires.
func (a Action) Validate() error {
	hasBody := strings.TrimSpace(a.Body) != ""
	switch a.Type {
	case ActionPostComment:
		if !hasBody {
			return fmt.Errorf("post_comment requires a body")
		}
		return nil
	case ActionReplyToComment:
		if a.CommentID == 0 {
			return fmt.Errorf("reply_to_comment requires comment_id")
		}
		if !hasBody {
			return fmt.Errorf("reply_to_comment requires a body")
		}
		return nil
	case ActionSubmitReview:
		switch a.Event {
		case "approve":
			return nil
		case "comment", "request_changes":
			// GitHub rejects COMMENT / REQUEST_CHANGES reviews without a body.
			if !hasBody {
				return fmt.Errorf("submit_review %s requires a body", a.Event)
			}
			return nil
		}
		return fmt.Errorf("submit_review event must be approve, comment, or request_changes (got %q)", a.Event)
	default:
		return fmt.Errorf("unknown action type %q", a.Type)
	}
}

const actionFence = "```prtea-action"

// ExtractActions parses trailing ```prtea-action fenced blocks from an agent
// message. It returns the message with the blocks removed and the parsed
// actions. Malformed blocks are left in place so the user can see them.
//
// The closing fence must sit at the start of a line: action bodies routinely
// contain ``` (code snippets, suggestion blocks) as JSON-escaped text, and
// those must not terminate the block early.
func ExtractActions(message string) (string, []Action) {
	var actions []Action
	remaining := message

	for {
		start := strings.LastIndex(remaining, actionFence)
		if start == -1 {
			break
		}
		// The opening fence must itself be at the start of a line.
		if start > 0 && remaining[start-1] != '\n' {
			break
		}
		blockBody := remaining[start+len(actionFence):]
		end := closingFenceIndex(blockBody)
		if end == -1 {
			break
		}
		// Only strip blocks that are trailing (nothing but whitespace after).
		after := blockBody[end:]
		after = after[strings.Index(after, "```")+3:]
		if strings.TrimSpace(after) != "" {
			break
		}

		var action Action
		if err := json.Unmarshal([]byte(blockBody[:end]), &action); err != nil {
			break
		}
		if err := action.Validate(); err != nil {
			break
		}

		actions = append([]Action{action}, actions...)
		remaining = strings.TrimRight(remaining[:start], " \t\n")
	}

	return remaining, actions
}

// closingFenceIndex finds the offset in blockBody where a line-anchored
// closing ``` fence begins (the index of its preceding newline's end).
// Returns -1 if no line starts with ```.
func closingFenceIndex(blockBody string) int {
	offset := 0
	rest := blockBody
	for {
		idx := strings.Index(rest, "\n```")
		if idx == -1 {
			return -1
		}
		lineStart := idx + 1
		line := rest[lineStart:]
		if lineEnd := strings.IndexByte(line, '\n'); lineEnd != -1 {
			line = line[:lineEnd]
		}
		if strings.TrimSpace(line) == "```" {
			return offset + lineStart
		}
		offset += lineStart + 3
		rest = rest[lineStart+3:]
	}
}
