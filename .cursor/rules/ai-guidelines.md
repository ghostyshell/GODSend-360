# AI coding guidelines

Apply to all tasks here, across Claude Code, OpenCode, and Cursor.

## Coding discipline — adapted from Andrej Karpathy's CLAUDE.md
Source: https://github.com/multica-ai/andrej-karpathy-skills/blob/main/CLAUDE.md

1. **Think before coding.** State assumptions explicitly; if uncertain, ask. Surface confusion and tradeoffs instead of silently guessing.
2. **Simplicity first.** Write the minimum code that solves the problem — nothing speculative. If a simpler approach exists, say so. Ask: would a senior engineer find this overcomplicated?
3. **Surgical changes.** Touch only what the task requires — every changed line should trace directly to the request. Don't refactor or "improve" unrelated code; match the existing style.
4. **Goal-driven execution.** Turn vague tasks into testable objectives (e.g. "add validation" → write tests for invalid inputs, then make them pass) and verify before finishing.

These favor caution over speed; use judgment on trivial work.

## Minimalist code — Ponytail
Source: https://github.com/DietrichGebert/ponytail — "the best code is the code you never wrote."
Before writing code, walk the ladder and stop at the first that works:
1. Does this need to exist at all? (YAGNI)
2. Is it in the standard library?
3. Is it a native platform feature?
4. Is it an already-installed dependency?
5. Can it be one line?
6. Only then, write the minimum necessary.

Claude Code plugin: install once with `/plugin marketplace add DietrichGebert/ponytail` then `/plugin install ponytail@ponytail`, then review with `/ponytail-review`, `/ponytail-audit`, `/ponytail-debt`. (Cursor/OpenCode: copy the repo's rules files / add the plugin to `opencode.json`.)

## UI/UX work — ui-ux-pro-max
Source: https://github.com/nextlevelbuilder/ui-ux-pro-max-skill
This repo has user-facing UI. For any UI/UX task (pages, components, layouts, design systems, styling), use the **ui-ux-pro-max** skill — styles, color palettes, font pairings, and per-product design reasoning. In Claude Code it activates automatically; you can also run `python3 .claude/skills/ui-ux-pro-max/scripts/search.py "<product>" --design-system`.

## Use the specialized agents for quality
A large library of specialized subagents is installed for each tool — `~/.claude/agents/` (Claude Code), `~/.config/opencode/agents/` (OpenCode), and `~/.cursor/agents/` (Cursor). Use them as part of **every** change to the repos, not as an afterthought: pick the most specific agent for the task and chain them.

- **Review (always):** after any substantive change, run `code-reviewer`; for design/architecture changes, `architect-reviewer`.
- **Security:** `security-auditor` / `security-engineer` for auth, secrets, input handling, crypto, or user-facing surfaces.
- **Tests:** `test-automator` to add/extend tests; `qa-expert` for strategy.
- **Debugging:** `debugger`, `error-detective`. **Performance:** `performance-engineer`; queries → `database-optimizer` / `sql-pro`.
- **Refactors:** `refactoring-specialist`; multi-file or cross-repo → `codebase-orchestrator`.
- **Stack specialists** (match the repo): e.g. `golang-pro`, `typescript-pro`, `javascript-pro`, `node-specialist`, `react-specialist`, `nextjs-developer`, `python-pro`.

Before treating a change as done, run at least `code-reviewer` (plus `security-auditor` for sensitive changes). Browse the agent dirs above for the full set (154 agents).
