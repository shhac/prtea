package ai

import (
	"fmt"
	"strings"
)

// OrientMessage is the canned first message sent when the user asks for an
// overview of the PR (the "analyze" keybinding).
const OrientMessage = "Orient me on this PR: what does it do and why, what are the riskiest parts, " +
	"and where should I start reading? Keep it under ~300 words and lead with the one-paragraph gist."

const systemPreamble = `You are an assistant embedded in prtea, a terminal UI for reviewing GitHub pull requests. You help the user understand PRs, answer questions about them, and act on them.

Constraints:
- Your sandbox is read-only with NO network access. You cannot run gh, git push, or fetch anything remote. Do not try.
- Reply in concise GitHub-flavored markdown suitable for a narrow terminal panel.

Performing GitHub actions:
You cannot perform GitHub actions yourself, but prtea can. When the user asks you to do something on GitHub, end your reply with one fenced code block per action, tagged prtea-action, containing only a single JSON object. The action blocks must be the very last thing in your reply — any text after them breaks parsing. prtea shows each proposed action to the user for confirmation before executing it. Never invent an action the user didn't ask for.

Available actions:
- {"type": "post_comment", "body": "<markdown>"} — post a comment on the PR
- {"type": "reply_to_comment", "comment_id": <id>, "body": "<markdown>"} — reply to an inline review comment (use the comment IDs from the PR context)
- {"type": "submit_review", "event": "approve"|"comment"|"request_changes", "body": "<markdown>"} — submit a PR review`

// buildThreadPrompt assembles the initial prompt for a new thread: system
// preamble, per-repo instructions, PR context, and the first user message.
func buildThreadPrompt(input ThreadInput) string {
	var b strings.Builder

	b.WriteString(systemPreamble)

	if input.RepoPath != "" {
		b.WriteString("\n\nA local checkout of this repository is your working directory. " +
			"Explore files with shell commands when the diff alone is not enough context. " +
			"The checkout may be on a different branch or commit than this PR — where local " +
			"files disagree with the PR diff below, trust the diff.")
	} else {
		b.WriteString("\n\nNo local checkout is available; work from the PR context below.")
	}

	if input.CustomPrompt != "" {
		b.WriteString("\n\nRepository-specific instructions:\n")
		b.WriteString(input.CustomPrompt)
	}

	fmt.Fprintf(&b, "\n\n--- PR context: %s/%s #%d ---\n", input.Owner, input.Repo, input.PRNumber)
	b.WriteString(input.PRContext)

	b.WriteString("\n\n--- User message ---\n")
	b.WriteString(input.Message)

	return b.String()
}

// buildFollowUpPrompt assembles a resumed-turn prompt: optional refreshed
// context followed by the user message.
func buildFollowUpPrompt(input MessageInput) string {
	if input.ContextDelta == "" {
		return input.Message
	}

	var b strings.Builder
	b.WriteString("--- Updated context ---\n")
	b.WriteString(input.ContextDelta)
	b.WriteString("\n\n--- User message ---\n")
	b.WriteString(input.Message)
	return b.String()
}
