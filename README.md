# prtea

A terminal dashboard for reviewing GitHub PRs with AI-powered analysis.

<!-- TODO: Add screenshot/GIF of the three-panel layout -->

Three-panel TUI built with [Bubbletea](https://github.com/charmbracelet/bubbletea): browse your PRs, read diffs, and chat with Claude about the changes — all without leaving the terminal.

## Features

- **Three-panel layout** — PR list, diff viewer, and AI chat side by side with toggleable panels and zoom
- **AI-powered analysis** — one-key PR analysis with risk assessment, architecture impact, and line-level comments
- **Interactive chat** — ask Claude questions about the PR with streaming markdown responses and hunk-specific context
- **Hunk selection** — select specific diff hunks to focus AI chat and analysis on what matters
- **Review submission** — approve, request changes, or leave review comments with an integrated Review tab
- **CI status** — dedicated tab showing check results grouped by status
- **Review status** — per-reviewer approval breakdown with visual badges
- **Comments** — read and post PR comments with full markdown rendering
- **Custom prompts** — per-repo review instructions for tailored analysis
- **Vim-style navigation** — j/k, Ctrl+d/u, g/G, and modal editing in chat

## Prerequisites

- [Go](https://go.dev/) 1.25+
- [GitHub CLI](https://cli.github.com/) (`gh`) — authenticated with `gh auth login`
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code) (`claude`) — optional, required for AI analysis and chat

## Installation

### Homebrew

> Coming soon

### GitHub Releases

> Coming soon

### Build from Source

```bash
git clone https://github.com/shhac/prtea.git
cd prtea
make build
```

The binary is written to `bin/prtea`. Move it somewhere on your `$PATH`:

```bash
cp bin/prtea /usr/local/bin/
```

## Usage

```bash
prtea
```

Launch from any directory. The PR list loads your review requests and authored PRs from GitHub.

**Typical workflow:**

1. Browse PRs in the left panel — switch between "To Review" and "My PRs" tabs with `h`/`l`
2. Press `Enter` to select a PR and jump to the diff viewer
3. Navigate the diff with `j`/`k`, jump between hunks with `n`/`N`
4. Press `a` to run AI analysis, or select specific hunks with `s` and press `Enter` to chat about them
5. Switch to the Review tab with `l` and submit your review (approve, comment, or request changes)

## Keybindings

Press `?` at any time to see the full keybinding reference.

### Global

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Switch panels |
| `1` / `2` / `3` | Jump to panel |
| `[` / `\` / `]` | Toggle left/center/right panel |
| `z` | Zoom focused panel |
| `r` | Refresh (PR list / selected PR) |
| `a` | Analyze PR |
| `o` | Open in browser |
| `?` | Toggle help |
| `q` | Quit |

### PR List

| Key | Action |
|-----|--------|
| `h` / `l` | Prev/next tab |
| `j` / `k` | Move up/down |
| `Space` | Select PR |
| `Enter` | Select PR + focus diff |

### Diff Viewer

| Key | Action |
|-----|--------|
| `h` / `l` | Prev/next tab (Diff, PR Info, CI) |
| `j` / `k` | Scroll up/down |
| `Ctrl+d` / `Ctrl+u` | Half page down/up |
| `n` / `N` | Next/prev hunk |
| `g` / `G` | Jump to top/bottom |
| `s` / `Space` | Select/deselect hunk |
| `Enter` | Select hunk + focus chat |
| `S` | Select/deselect all file hunks |
| `c` | Clear selection |

### Chat (Normal Mode)

| Key | Action |
|-----|--------|
| `h` / `l` | Prev/next tab (Chat, Analysis, Comments, Review) |
| `j` / `k` | Scroll history |
| `C` | New chat (clear conversation) |
| `Enter` | Enter insert mode |

### Chat (Insert Mode)

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Esc` | Exit insert mode |

### Review Tab

| Key | Action |
|-----|--------|
| `Enter` | Edit review body / submit review |
| `Esc` | Exit textarea |
| `Tab` / `Shift+Tab` | Cycle focus (textarea, action, submit) |
| `j` / `k` | Cycle review action (approve, comment, request changes) |

## Configuration

Config file location: `~/.config/prtea/config.json`

```json
{
  "reposPath": "~/repos",
  "claudeTimeoutMs": 120000,
  "pollIntervalMs": 60000
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `reposPath` | `~/repos` | Path to local repository clones |
| `claudeTimeoutMs` | `120000` | AI analysis timeout in milliseconds |
| `pollIntervalMs` | `60000` | Auto-refresh interval in milliseconds |

### Custom Prompts

Add per-repository review instructions by creating markdown files in `~/.config/prtea/prompts/`:

```
~/.config/prtea/prompts/{owner}_{repo}.md
```

These are automatically included when analyzing PRs for that repository.

## Development

### Running Tests

```bash
go test ./...
```

Tests cover pure functions (panel layout, CI status computation, diff parsing, review deduplication) and mock-based GitHub client methods using injectable `CommandRunner`. No external services or `gh` CLI needed for tests.

### Project Structure

```
cmd/prtea/main.go        Entry point
internal/ui/              Bubbletea UI layer (panels, layout, styles, keys)
internal/github/          GitHub API client (gh CLI based, with CommandRunner injection)
internal/claude/          Claude CLI subprocess (analysis + chat + caching)
internal/config/          Config file management
internal/git/             Git operations (clone, fetch, SHA lookup)
```

## License

[MIT](LICENSE)
