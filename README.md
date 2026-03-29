# HerdingLlamas

A CLI tool that makes LLMs argue with each other so you don't have to argue with them yourself.

HerdingLlamas orchestrates structured conversations between AI models (Claude and Codex) in a shared channel, then summarizes the result. Instead of asking one model and hoping it didn't hallucinate, you pit two against each other and let them sort it out.

## Why

A single LLM will confidently give you one answer. Two LLMs forced to respond to each other will surface disagreements, edge cases, and blind spots that neither would find alone. The interesting stuff lives in the friction.

## Install

```
go build ./cmd/herd
```

Requires Claude Code and/or Codex CLI sessions available on your system, plus Python 3 (used internally for stop hook scripts).

## Modes

### Debate

Two agents research a question, argue their positions, and converge on an answer. Use this when you want to stress-test a decision, compare tradeoffs, or get a well-reasoned answer to a technical question you don't want to take one model's word on.

Both agents have symmetric roles — they research, post arguments, respond to each other, and concede when the other makes a stronger point.

```
herd debate "Is TDD actually worth the overhead for small teams?"
```

```
herd debate "Monorepo vs polyrepo for a 15-person startup" --models claude,codex --max-turns 12
```

For scripting or CI pipelines, `--json` runs headless (no TUI) and writes structured JSON to stdout:

```
herd debate "Should we migrate to gRPC?" --json --max-turns 8 > result.json
```

### Interrogate

Exhaustive plan validation. Use this when you have a design doc, implementation plan, or technical proposal and you want it torn apart before you start building. The goal isn't consensus — it's finding every gap, unstated assumption, and implementation-level issue in a single session.

Two agents with asymmetric roles:

- **Advocate** (first model): Reads the plan and codebase deeply, builds the strongest evidence-based defense of the plan, and proactively surfaces gaps found during their deep read. Defends with specifics — file paths, function signatures, actual types. When they can't defend something, they confirm the gap and propose a fix.
- **Interrogator** (second model): Systematically probes the plan across a 10-dimension checklist: assumptions vs. reality, data flow, integration boundaries, failure modes, state/consistency, external dependencies, operational concerns, performance, sequencing, and gaps/ambiguity. Cannot conclude until every dimension is addressed. Grounded in the plan's intent — asks not just "is this component sound?" but "does this plan actually achieve what it's trying to achieve?"

Both agents do real research — web searches for known pitfalls, best practices, war stories from similar implementations — not just code reading.

```
herd interrogate "Review the design in docs/plans/migration-design.md against the existing codebase" --workdir ~/repos/my-project
```

```
herd interrogate "Evaluate the auth rewrite proposal in DESIGN.md — check every integration point against the current middleware" --json
```

The summary output is structured as a plan assessment: confirmed strengths, identified gaps (with severity and recommended fixes), uncovered dimensions, and implementation recommendations.

### Explore

Lateral thinking mode. Use this when you want to see what you're not seeing — find non-obvious connections, structural parallels from unrelated domains, and ideas that wouldn't come up in a direct analysis.

Two agents with intentionally asymmetric roles:

- **Connector** (first model): Searches *unrelated* domains for structural analogies. Explicitly forbidden from researching the topic directly. Looks at biology, economics, urban planning, game theory — whatever shares the same underlying pattern.
- **Critic** (second model): Researches the topic directly and stress-tests the Connector's analogies against reality. When an analogy implies something that doesn't exist yet, the Critic's job is to figure out what it would take to build it.

The asymmetry is the point. Same-role agents converge too quickly. Different information access + different cognitive tasks = more surprising output.

```
herd explore "How should we approach real-time collaboration in our editor?"
```

### Summary

All modes automatically generate a summary when they finish. The summary format adapts to the mode — debate summaries synthesize the answer, interrogation summaries produce a structured plan assessment, and explore summaries extract actionable implications. To skip this, pass `--no-summary`.

You can also generate a summary for any past session:

```
herd summary --latest
herd summary --debate abc123
herd summary --latest --json
```

## How It Works

Agents don't communicate through piped stdout. Each agent gets CLI tools (`herd channel post`, `herd channel read`, `herd channel wait`, `herd channel conclude`) baked into their system prompt, and all communication happens through explicit command invocations against a shared SQLite database.

```
                    ┌──────────────────┐
                    │   Debate Engine   │
                    │  monitor + nudge  │
                    └────────┬─────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
     ┌────────▼────────┐     │    ┌────────▼────────┐
     │    Agent 1       │     │    │    Agent 2       │
     │  (Claude/Codex)  │     │    │  (Claude/Codex)  │
     └────────┬────────┘     │    └────────┬────────┘
              │              │              │
              │    ┌─────────▼──────────┐   │
              └───►│  Channel (SQLite)   │◄──┘
                   │ messages, cursors   │
                   │ turns, conclusions  │
                   └─────────┬──────────┘
                             │
                   ┌─────────▼──────────┐
                   │  TUI (Bubble Tea)   │
                   │  live message view  │
                   └────────────────────┘
```

The engine polls the database every second. When agent A posts a message, the engine nudges agent B with a notification about unread messages. Agents read, think, research, and post on their own schedule. Turn numbers are assigned atomically to prevent races.

Stop hooks keep agents from wandering off mid-debate. A state file tracks whether there are still unread messages or running agents, and blocks premature session exit.

Everything persists to `~/.herdingllamas/debates.db`, so you can summarize old sessions or inspect the transcript later.

## CLI Reference

### `herd debate [question]`

| Flag | Default | Description |
|------|---------|-------------|
| `--models` | `claude,codex` | Participating models |
| `--max-turns` | `0` (unlimited) | Stop after N turns |
| `--max-duration` | `0` (unlimited) | Stop after duration (e.g. `30m`) |
| `--workdir` | `.` | Working directory for agent sessions |
| `--json` | `false` | Output results as JSON to stdout (no TUI) |
| `--no-summary` | `false` | Skip automatic summary after debate ends |

### `herd interrogate [plan description]`

Same flags as `debate`. First model becomes the Advocate, second becomes the Interrogator.

### `herd explore [topic]`

Same flags as `debate`. First model becomes the Connector, second becomes the Critic.

### `herd summary`

| Flag | Default | Description |
|------|---------|-------------|
| `--debate` | | Specific debate ID |
| `--latest` | `false` | Summarize most recent session |
| `--json` | `false` | Output summary as JSON |

### `herd channel <subcommand>`

Used by agents internally. You probably won't run these yourself, but they're regular CLI commands if you want to poke at a running debate.

| Subcommand | Flags | What it does |
|------------|-------|-------------|
| `post` | `--debate`, `--from` | Post a message to the channel |
| `read` | `--debate`, `--agent` | Read unread messages, advance cursor |
| `wait` | `--debate`, `--agent`, `--timeout` | Block until opponent responds |
| `conclude` | `--debate`, `--from` | Propose ending (needs mutual agreement) |

## The TUI

When a debate or exploration is running, you get a live terminal view of the conversation. Messages appear as they're posted, color-coded by agent. Scroll with arrow keys, quit with `q`.

The header shows status (LIVE/ENDED), active agent count, message count, and elapsed time. The footer shows the debate ID so you can reference it later for `herd summary`.

## Project Structure

```
cmd/herd/          CLI commands (debate, interrogate, explore, summary, channel)
internal/agent/    Agent lifecycle, session adapters, system prompts
internal/debate/   Engine orchestration, config, stop hooks
internal/store/    SQLite persistence (messages, cursors, conclusions)
internal/tui/      Bubble Tea terminal interface
```

## License

MIT
