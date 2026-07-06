# Plan: prtea codex overhaul

Goal: overhaul prtea so the AI sidekick runs on the Codex CLI (safe, read-only,
quota-friendly), the UI sheds its accumulated modal sprawl, and the human code
review UX becomes first-class. Three phases, each landing independently on
`main` with tests green, followed by an improve-code-structure pass.

North star: prtea is a fast TUI for triaging and acting on PRs. The AI is a
codex-backed chat sidekick (explain / answer / propose actions) — not an
auto-reviewer. All GitHub reads/writes go through prtea's `gh`-based client;
codex never gets network or write access.

## Why (context)

- `claude -p --allowedTools Read,Glob,Grep,Bash` (internal/claude/analyzer.go)
  is an unattended agent with auto-approved shell — reviewing a hostile PR
  feeds attacker-controlled text to it. Unacceptable risk.
- The structured-JSON analysis and one-shot AI review generation are brittle
  (prompt-enforced schemas, brace-extraction fallback) and duplicate what
  proper review skills do better. Cut.
- Chat re-sends full history with `len/3` token estimation. Codex has real
  session resume (`codex exec resume <thread-id>`), which deletes all of that.

## Codex engine facts (verified locally, codex-cli 0.138.0)

- `codex exec --json` emits JSONL events on stdout as they happen:
  - `{"type":"thread.started","thread_id":"..."}` — first line, capture for resume
  - `{"type":"turn.started"}`
  - `{"type":"item.started"|"item.completed","item":{"id","type","..."}}`
    - item.type `command_execution`: `command`, `aggregated_output`, `exit_code`, `status`
    - item.type `agent_message`: `text` (whole message at completion)
    - item.type `reasoning`: summary text (may appear; render as dim activity)
  - `{"type":"turn.completed","usage":{input_tokens,cached_input_tokens,output_tokens,...}}`
  - `turn.failed` / `error` events on failure — detect these, not exit codes.
- Message-level streaming (no token deltas). The activity feed (commands run,
  intermediate messages) is the progress UI.
- Prompt via **stdin** (codex always reads stdin; argv would hit ARG_MAX on
  big diffs). When passing prompt as arg AND stdin is piped, stdin is appended
  as a `<stdin>` block — we will pass the prompt entirely on stdin and use
  `-` sentinel or no-arg form.
- Resume: `codex exec resume <THREAD_ID> --json ...` — does NOT accept
  `-s/--sandbox` (mode carries over from session creation).
- Sandbox: always `--sandbox read-only`. Network is unavailable in read-only.
- Model: `gpt-5.5` default; `-c model_reasoning_effort="low|medium|high"`.
- `-C <dir>` sets working root; requires git repo unless --skip-git-repo-check.
- Errors: non-zero exit + stderr; with `--json` prefer turn.failed/error events.

## Phase 1 — internal/ai (codex engine) replaces internal/claude

### Package layout

- `internal/ai/engine.go` — public surface:
  - `type Event` — normalized engine event for the UI:
    `Kind` (thinking|command|message|action-proposal|usage|error|done),
    plus fields (Command, Output, ExitCode, Text, Action, Usage...).
  - `type Engine interface { StartThread(ctx, ThreadInput) (<-chan Event, error); Send(ctx, threadID, message string) (<-chan Event, error) }`
    (exact shape may become funcs returning (threadID string, events <-chan Event, err error))
  - Bubbletea integration: UI goroutine adapter converts chan Event → tea.Msg.
- `internal/ai/codex.go` — CodexEngine: builds argv, pipes prompt via stdin,
  parses JSONL events, maps to Event. Reuses the injectable
  CommandExecutor/Process pattern from internal/claude/executor.go (port it).
- `internal/ai/prompts.go` — system preamble + PR context assembly + canned
  "orient me" prompt + action-protocol instructions + per-repo custom prompt
  loading (keep `~/.config/prtea/prompts/{owner}_{repo}.md`).
- `internal/ai/actions.go` — the prtea-action protocol:
  - Codex is instructed: to perform a GitHub action, end the final message with
    a fenced block ```prtea-action\n{JSON}\n``` .
  - Action JSON: `{"type":"post_comment"|"reply_to_comment"|"submit_review", ...fields}`
    (start small: post_comment, reply_to_comment{commentID}, submit_review{event,body}).
  - Parser extracts trailing action block(s) from the final agent_message;
    emits Event{Kind:action-proposal}. UI renders confirm prompt; on confirm,
    executes via GitHubService and reports result back into the chat log
    (locally, not to codex).
- `internal/ai/store.go` — thread persistence: `{owner}_{repo}_{pr}.json` →
  `{threadID, updatedAt, transcript []DisplayMessage}`. Transcript is for
  redisplay only; codex holds the real conversation state server/session-side.
- `internal/ai/finder.go` — locate codex binary (PATH + ~/.local/bin fallback),
  port of claude finder.
- Repo context: if cwd (or configured root) is a git repo whose origin matches
  the PR's owner/repo → run with `-C <that dir>` so codex can explore files;
  else embed diff+metadata in the prompt only (no -C, --skip-git-repo-check
  not needed since we run -C only when it IS a repo).

### UI rewiring

- `internal/ui/interfaces.go`: drop AIAnalyzer + AIChatService; add one
  `AIEngine` interface over internal/ai.
- Chat panel: render transcript = user msgs + activity feed lines (dim: "▸ ran
  `cmd`", "▸ thinking…") + assistant markdown messages + action-proposal
  confirm UI (y/n keys while proposal pending).
- `a` (Analyze) key → sends canned orient-me message into the chat thread
  (creates thread if none). Analysis tab becomes unnecessary (full removal in
  Phase 2; Phase 1 may stub it to redirect to chat to keep diff small — no:
  Phase 1 removes analysis tab code paths that depend on deleted types; do the
  minimal removal here, cosmetic consolidation in Phase 2).
- "Did they address my comment": ensure PR context includes inline comments /
  review threads + latest commit list so the question is answerable. Context
  refresh: on each Send, prepend a short "context delta" (new comments/commits
  since thread start) — keep simple: include current comments snapshot in
  StartThread; Send passes user text only, plus optional hunk selection block.
- Delete: internal/claude entirely (analyzer, prompts schemas, chat history
  replay, token estimation, stream-delta renderer, analysis stream renderer),
  AI review generation path into review tab / diff comments.
- Demo mode: fake AIEngine emitting scripted events (thinking → command →
  message) so --demo demonstrates chat + activity feed offline.
- Config: replace claude fields with `codexModel` (default "gpt-5.5"),
  `codexReasoningEffort` (default "medium"), keep `aiTimeoutMs` (rename from
  claudeTimeoutMs with backward-compat read). Drop maxTurns/token knobs.
- Notify: keep desktop notification on long-running turn completion.

### Verification

- `go test ./...` green; new tests: JSONL event parsing (golden lines from the
  real run), action block extraction, thread store round-trip, prompt assembly.
- Live smoke: run a real StartThread against this repo with a tiny prompt.
- Demo drive: `prtea --demo`, exercise chat with fake engine.

## Phase 2 — UI simplification

- Drive `prtea --demo` (PTY) and catalogue friction before/after.
- Remove Analysis tab remnants; right panel tabs = Chat / Comments / Review.
- Remove settings panel (721 lines) → config file + command palette entries
  for the few runtime toggles that matter (if any).
- Unify tab navigation (h/l consistent across panels), clearer focus styling.
- Trim app.go/app_handlers.go routing where the removals allow.
- Keybinding/help updates; README updates.

## Phase 3 — Human review UX

- Pending review basket: comment on current diff line/hunk (key: `c` in diff
  viewer) → comment editor overlay → basket entry {path, line, body}.
- Review tab: shows basket entries (editable/deletable), review body textarea,
  verdict cycle, submit → SubmitReviewWithComments (already exists in
  GitHubService).
- Review threads: show resolved/unresolved state on inline comments; feed
  thread state into AI chat context ("did they address my comment").
- AI stays out of review generation; the propose/confirm machinery already
  allows "draft a comment" via chat if asked. (A "fill basket with AI" action
  is explicitly deferred.)

## Wiring facts (from codebase map)

- All claude construction is in ui.NewApp (app.go:96-155); main.go is clean.
- Only AnalyzeDiffStream, AnalyzeForReview, ChatStream have live call sites;
  Analyze/AnalyzeDiff are dead interface members.
- Streaming: goroutines push tea.Msg onto chatStreamChan/analysisStreamChan on
  PRSession, drained by listenForStream (commands.go:342). Reuse this pattern:
  one aiEventChan carrying normalized ai.Event msgs.
- claude.InlineReviewComment is structurally identical to
  github.ReviewCommentPayload — replace all UI uses (PendingInlineComment,
  overlays, diff viewer AI comment fields) with the github type; delete the
  AI-sourced comment paths entirely.
- AnalysisStore (cached structured analysis) is deleted with the Analysis tab;
  orient-me output lives in the chat transcript (ai thread store).
- Demo mode has NO ai faking today — add a scripted demo Engine so --demo
  shows the chat/activity-feed experience.
- Config: ClaudeTimeout/claudeTimeoutMs + 4 knobs (MaxChatHistory,
  MaxPromptTokens, ChatMaxTurns, AnalysisMaxTurns) — replace with aiTimeoutMs
  (back-compat read from claudeTimeoutMs), codexModel, codexReasoningEffort.
  Settings panel AI section shrinks to timeout (model/effort are config-file).
- User-facing strings "Claude"/"Claude AI"/"Claude is thinking..." across
  chat_tab, diff_comments, comment_overlay, command_mode → "AI"/"Codex".
- Files deleted in Phase 1: internal/claude/* (all), internal/ui/analysis_tab.go,
  internal/ui/analysis_stream_renderer.go (+tests).

## Milestones / commits

- Commit per coherent step within phases (gm conventional commits), e.g.:
  - feat[ai]: add codex engine with JSONL event stream
  - feat[ui]: rewire chat panel to ai engine events
  - feat[ai]: action propose/confirm protocol
  - chore[claude]: remove claude analyzer and review generation
  - ...
- After each phase: run improve-code-structure skill, apply, commit.

## Risks / open points

- codex CLI flag drift (moves fast) — pin behaviors behind CodexEngine; tests
  use recorded JSONL.
- Large diffs: stdin fine, but cap embedded diff at ~N KB with truncation
  notice (context window protection) — cheap guard, no token math.
- Sandbox blocks network: "perform this action" MUST go through the action
  protocol; prompt must make codex aware it cannot run gh itself.
- resume + read-only: sandbox carried over from thread creation — good.
- Old chat sessions on disk (messages JSON) — drop with a one-line note in
  commit message; format changes to thread store.
