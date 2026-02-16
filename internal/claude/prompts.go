package claude

import (
	"fmt"
	"os"
	"strings"
)

// -- Analysis prompts --

func buildAnalysisPrompt(promptsDir string, input AnalyzeInput) string {
	body := input.PRBody
	if body == "" {
		body = "No description provided."
	}

	customPrompt := loadCustomPrompt(promptsDir, input.Owner, input.Repo)

	return fmt.Sprintf(`You are reviewing PR #%d: "%s".

PR description:
%s

Instructions:
1. Run `+"`git diff origin/%s...origin/%s`"+` to see all changes in this PR.
2. For each changed file, read the full file on the %s branch to understand context — follow imports, check callers, understand the module's role.
3. Produce a thorough code review as structured JSON output.

Focus on: correctness, security, performance, maintainability, and test coverage. Be specific with line numbers when possible.
%s
IMPORTANT: Your final response must be ONLY valid JSON matching this schema (no markdown, no wrapping):
%s`,
		input.PRNumber, input.PRTitle,
		body,
		input.BaseBranch, input.HeadBranch,
		input.BaseBranch,
		customPrompt,
		analysisJSONSchema,
	)
}

func buildDiffAnalysisPrompt(promptsDir string, input AnalyzeDiffInput) string {
	body := input.PRBody
	if body == "" {
		body = "No description provided."
	}

	customPrompt := loadCustomPrompt(promptsDir, input.Owner, input.Repo)

	return fmt.Sprintf(`You are reviewing PR #%d in %s/%s: "%s".

PR description:
%s

Here is the complete diff for this PR:

%s

Instructions:
1. Review all changes shown in the diff above.
2. Produce a thorough code review as structured JSON output.

Focus on: correctness, security, performance, maintainability, and test coverage. Be specific with line numbers when possible.
%s
IMPORTANT: Your final response must be ONLY valid JSON matching this schema (no markdown, no wrapping):
%s`,
		input.PRNumber, input.Owner, input.Repo, input.PRTitle,
		body,
		input.DiffContent,
		customPrompt,
		analysisJSONSchema,
	)
}

func buildReviewPrompt(promptsDir string, input ReviewInput) string {
	body := input.PRBody
	if body == "" {
		body = "No description provided."
	}

	customPrompt := loadCustomPrompt(promptsDir, input.Owner, input.Repo)

	return fmt.Sprintf(`You are generating a GitHub pull request review for PR #%d in %s/%s: "%s".

PR description:
%s

Here is the complete diff for this PR:

%s

Instructions:
1. Review all changes shown in the diff above.
2. Decide whether to approve, comment, or request changes.
3. Write an overall review body summarizing your assessment.
4. For specific issues, add inline comments targeting the exact file path and line number.
   - Use the NEW file line number (right side of the diff) for added/modified lines.
   - Only comment on lines that actually appear in the diff.
   - Each comment should be actionable and specific.
   - Focus on bugs, security issues, and significant improvements. Skip trivial style nits.
%s
IMPORTANT: Your final response must be ONLY valid JSON matching this schema (no markdown, no wrapping):
%s`,
		input.PRNumber, input.Owner, input.Repo, input.PRTitle,
		body,
		input.DiffContent,
		customPrompt,
		reviewJSONSchema,
	)
}

func loadCustomPrompt(promptsDir, owner, repo string) string {
	if promptsDir == "" {
		return ""
	}
	path := fmt.Sprintf("%s/%s_%s.md", promptsDir, owner, repo)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return "\nAdditional review instructions:\n" + string(data)
}

// analysisJSONSchema is the JSON schema that Claude must produce.
var analysisJSONSchema = `{
  "type": "object",
  "required": ["summary", "risk", "architectureImpact", "fileReviews", "testCoverage", "suggestions"],
  "properties": {
    "summary": { "type": "string" },
    "risk": {
      "type": "object",
      "required": ["level", "reasoning"],
      "properties": {
        "level": { "type": "string", "enum": ["low", "medium", "high", "critical"] },
        "reasoning": { "type": "string" }
      }
    },
    "architectureImpact": {
      "type": "object",
      "required": ["hasImpact", "description", "affectedModules"],
      "properties": {
        "hasImpact": { "type": "boolean" },
        "description": { "type": "string" },
        "affectedModules": { "type": "array", "items": { "type": "string" } }
      }
    },
    "fileReviews": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["file", "summary", "comments"],
        "properties": {
          "file": { "type": "string" },
          "summary": { "type": "string" },
          "comments": {
            "type": "array",
            "items": {
              "type": "object",
              "required": ["severity", "comment"],
              "properties": {
                "line": { "type": "number" },
                "severity": { "type": "string", "enum": ["critical", "warning", "suggestion", "praise"] },
                "comment": { "type": "string" }
              }
            }
          }
        }
      }
    },
    "testCoverage": {
      "type": "object",
      "required": ["assessment", "gaps"],
      "properties": {
        "assessment": { "type": "string" },
        "gaps": { "type": "array", "items": { "type": "string" } }
      }
    },
    "suggestions": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["title", "description"],
        "properties": {
          "title": { "type": "string" },
          "description": { "type": "string" },
          "file": { "type": "string" }
        }
      }
    }
  }
}`

// reviewJSONSchema is the JSON schema for AI-generated reviews.
var reviewJSONSchema = `{
  "type": "object",
  "required": ["action", "body", "comments"],
  "properties": {
    "action": { "type": "string", "enum": ["approve", "comment", "request_changes"] },
    "body": { "type": "string", "description": "Overall review comment summarizing the assessment" },
    "comments": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["path", "line", "body"],
        "properties": {
          "path": { "type": "string", "description": "Relative file path" },
          "line": { "type": "number", "description": "Line number in the new file (right side)" },
          "body": { "type": "string", "description": "Inline comment text" }
        }
      }
    }
  }
}`

// -- Chat prompts --

func buildChatPrompt(session *ChatSession, input ChatInput, maxTokens, maxHistory int) string {
	var b strings.Builder

	// System instruction (always included)
	systemPrefix := "You are helping review a pull request. Here is the context:\n\n"
	b.WriteString(systemPrefix)

	var instruction string
	if input.HunksSelected {
		instruction = "\n\nThe user has selected specific code hunks from the diff above. " +
			"Focus your answer primarily on these selected hunks. " +
			"Explain what the selected code does, flag potential issues, and suggest improvements.\n"
	} else {
		instruction = "\n\nAnswer questions about this PR based on the diff and metadata provided above.\n"
	}

	currentMsg := fmt.Sprintf("\nUser: %s\n\nRespond helpfully and concisely.", input.Message)

	// Calculate fixed token costs
	fixedTokens := estimateTokens(systemPrefix) + estimateTokens(instruction) + estimateTokens(currentMsg)
	contextTokens := estimateTokens(input.PRContext)

	// Determine which messages to include (most recent first, up to budget)
	messages := session.Messages
	if len(messages) > maxHistory {
		messages = messages[len(messages)-maxHistory:]
	}

	// Further trim messages if total exceeds token budget
	historyTokens := 0
	for _, msg := range messages {
		historyTokens += estimateTokens(msg.Content) + 10 // 10 for "User: " / "Assistant: " prefix
	}

	totalTokens := fixedTokens + contextTokens + historyTokens
	if totalTokens > maxTokens && len(messages) > 2 {
		// Drop oldest messages until we fit (keep at least the last 2 messages)
		for totalTokens > maxTokens && len(messages) > 2 {
			dropped := messages[0]
			messages = messages[1:]
			totalTokens -= estimateTokens(dropped.Content) + 10
		}
	}

	// If still over budget after trimming history, truncate the diff context
	prContext := input.PRContext
	if totalTokens > maxTokens {
		// Calculate how many tokens we can afford for the context
		availableContextTokens := maxTokens - fixedTokens - historyTokens
		if availableContextTokens < 0 {
			availableContextTokens = 0
		}
		maxContextChars := availableContextTokens * 3 // reverse the estimation
		if maxContextChars > 0 && maxContextChars < len(prContext) {
			prContext = prContext[:maxContextChars] + "\n\n[... diff truncated to fit context window ...]"
		}
	}

	b.WriteString(prContext)
	b.WriteString(instruction)

	for _, msg := range messages {
		if msg.Role == "user" {
			fmt.Fprintf(&b, "\nUser: %s", msg.Content)
		} else {
			fmt.Fprintf(&b, "\nAssistant: %s", msg.Content)
		}
	}

	b.WriteString(currentMsg)

	return b.String()
}

// estimateTokens returns a rough token count for a string.
// Code and diffs average ~3 chars per token; prose ~4 chars.
// We use 3 as a conservative estimate (overestimates slightly for prose).
func estimateTokens(s string) int {
	return len(s) / 3
}
