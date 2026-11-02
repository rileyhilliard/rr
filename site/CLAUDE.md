@AGENTS.md

## Claude Code

`AGENTS.md` (imported above) is the project's instruction file for every coding agent. Put project knowledge there, not here. This section is only for things specific to Claude Code.

- `.claude/README.md` describes the hooks. In short: destructive commands and credential-shaped writes are denied (ask the user instead of working around a deny), lint diagnostics for the file you just wrote arrive as context (fix them before moving on), and files you wrote are formatted when you stop.
- `.claude/rules/` (create it when needed) holds path-scoped guidance that loads when you work on matching files. Keep rules for enforcement-grade detail about one area; cross-cutting facts belong in `AGENTS.md`.
- Personal or machine-specific settings go in `.claude/settings.local.json` (gitignored), never in the shared `settings.json`.
- A subdirectory with its own `.claude/` can be used as a separate session root. Settings don't inherit into it, and `paths:` globs in its rules are relative to that subdirectory.
