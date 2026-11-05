# rr landing page: product spec

> Status: built. Kept here (not in the repo's gitignored `plans/`) so the reasoning behind the page ships with it.

### TL;DR
A one-page site plus docs for developers who run coding agents and have a spare machine or two. Using real rr output, it shows that test runs can leave the laptop, queue safely when several agents run at once, and shard across hosts. Then it gives one install command and an honest list of what you need.

### Problem
- **Who:** a developer running coding agents test-first (Claude Code, Codex), often in several worktrees at once. They have, or could have, a spare mini PC or workstation on their LAN or tailnet. Secondary reader: the agent itself, reading `llms.txt` or the docs while it sets rr up.
- **What:** they arrive from a launch post, steamfield.io or the Claude plugin, and land on a 400-line README. The core idea (agents' tests run on other machines and queue safely, from one command) and the proof for it are scattered through that README.
- **Evidence:** the README's "Built for coding agents" section and its problem statement, which calls multi-agent queueing the bigger reason rr exists. OpenData runs about 14,000 tests through `rr test`. The README's three-task run went from 97s to 52.9s. There's no visitor data yet, which is why analytics are in scope.
- **Why now:** v0.29.x is stable, ProxyJump support and the Homebrew cask just shipped, the launch copy is drafted, and steamfield.io links to rr with nowhere to go but GitHub.

### Job to be done
When my agents are fighting my laptop for CPU, ports and test databases, I want to see quickly whether rr can move their test runs to my spare machines without running a new platform, so I can try it today.
- Functional: understand the model (lock, sync, run, stream back), confirm my setup qualifies (SSH, rsync, a toolchain on the remote, no daemon), and copy an install command.
- Emotional: "this is a sharp, small tool, not infrastructure I'll have to babysit."

### Journey
| Moment | What happens |
|---|---|
| Entry | Links from launch posts, steamfield.io, the Claude plugin docs, and the GitHub repo header. |
| First impression (one viewport) | An H1 about the job, a lede that names agents, the brew install line with a copy button, and the top of the Monitor stage: three host cards whose graphs are already climbing, each naming the suite it's running. |
| Core action | Copy an install command (brew, script or go). |
| Feedback | A "Copied" state. Directly below it: `rr init`, and for Claude Code users, the plugin step. |
| Recovery | Without JS everything is static text: transcripts render as `<pre>`, copy buttons stay hidden and the commands can be selected. "No spare machine?" points to `rr host add --local`. |
| Return | Docs at `/docs/`, `llms.txt`, GitHub, the changelog. |

### Experience qualities
**Transparent** (you see exactly what rr prints), **Fast** (static pages, cheap demos), **Powerful** (dense rather than hand-holding). The tradeoff is density over guidance: the page doesn't teach SSH, it links to `/docs/ssh-setup/`.

### Page outline
Each section answers one visitor question.
1. **Hero: "What is it?"**
   - H1: "Load-balance your coding agents' test runs across your own machines." Set in Instrument Sans, not mono, so it doesn't wrap to five lines.
   - Lede names agents: rr syncs your tree, locks a free host, runs the command and streams output back, so parallel coding agents stop fighting over one laptop. Single Go binary, no daemon.
   - `brew install rileyhilliard/tap/rr` with a copy button, plus a GitHub link.
   - The **Monitor stage** (see Design): a recreation of `rr monitor` replaying a real parallel test run across three hosts, with the `rr test` transcript beneath it.
2. **"Why would I need this?"** The README problem in three concrete lines: agents in separate worktrees on one laptop fail tests over CPU, ports and test databases.
3. **"What if two agents hit it at once?"** The **Queue board**, a host table where you can press "Add agent" (up to 4 agents on 3 hosts). This is the real `findAvailableHost` behavior (`internal/cli/loadbalance.go:184`):
   - A new agent takes the next unlocked host in `hosts:` order.
   - When every host is locked, it shows the real wait message (`loadbalance.go:482`, "All hosts locked; waiting up to 1m") and the `lock`/`waiting` JSON event.
   - When a run finishes, a waiting agent gets that host: whichever retries first (every 2s, `roundRobinWait`), not the longest waiter.
   - This is a table, not lanes, so it doesn't repeat Steamfield's `RrDemo`.
4. **"How much faster?"** Static content, no island:
   - The real `Parallel Execution Summary` from the README's three-task example, beside its sequential total (97s vs 52.9s).
   - Two sentences from README line 54: the floor is your slowest shard.
   - One line: "rr hands out tasks in the order you list them, so put the long ones first." (FIFO, verified in the orchestrator.)
5. **"What if I'm not home?"** The README's Home / Coffee shop / Host down fallback table as a static table, with aliases dialed in parallel and earlier ones preferred. No island.
6. **"What will I actually type?"** The **Commands** section. A tab list on the left picks a command; the terminal component on the right plays it and shows its real captured output.
   - `rr init`: writing `.rr.yaml`.
   - `rr doctor`: checks passing, with alias latency.
   - `rr status`: hosts, which alias connected, and lock holders.
   - `rr run "go test ./internal/lock/..."`: an ad-hoc command, showing connect → lock → sync → exec.
   - `rr test`: a named parallel task and its summary.
   - `rr logs`: recent runs, plus the `summary.json` path.

   Each tab has a one-line caption saying when you'd use it, and a copy button for the command. Without JS, all six render as stacked static transcripts.
7. **"How does my agent use it?"** The **Agent session**, a Claude-Code-style transcript in the terminal component (pattern from stacks-api `HomeCliDemo.tsx`: `❯` user turn, `Bash(...)` tool calls, `⎿` abbreviated output), working in the rr repo itself.
   1. The user asks the agent to fix a failing lock test.
   2. The agent runs the **filtered** test: `Bash(rr run "go test ./internal/lock/... -run TestLockAcquisition")`.
   3. The JSON result comes back with `status: "failure"`, and `details.failures` gives `file:line` plus the message. The agent reads it.
   4. The agent edits the file and reruns the same filtered command, which now passes.
   5. The agent runs the **full suite**: `Bash(rr test)`. Phase events stream, the run fans out across hosts, and the final `result` event reports success, with `details.summary` counts.
   6. The agent replies with a one-line summary.

   Under the transcript sits a short list of real error codes it might see instead (`LOCK_HELD`, `SSH_TIMEOUT`, …, from `internal/cli/json.go`), noting that the agent branches on the code, not the message. Then the Claude Code plugin as an add-on step: install rr first, then `/plugin marketplace add …`, `/plugin install rr@rr`, `/rr:setup`.
8. **"What does setup look like?"** The minimal two-file setup: a global host plus a `.rr.yaml` with one task (README lines 295–326). The proof line under it: "OpenData runs ~14,000 tests through `rr test`", linking to the OpenData config in the docs.
9. **"Why not X?"** The README comparison table (rsync && ssh, GitHub Actions, Ansible, DevPod/Tilt, VS Code Remote) as a real `<table>`.
10. **Install.**
   - Tabs for Homebrew, script and Go.
   - Beside the tabs, a **What you need** block:
     - SSH and rsync on both ends.
     - Your toolchain on the remote (`require:` / `rr provision`).
     - macOS or Linux, or Windows via WSL.
     - A link to `/docs/ssh-setup/`.
   - Then "Next: `rr init`", the Claude Code step, and "No spare machine? `rr host add --local` still queues your agents."
   - The version is read from `CHANGELOG.md` at build time. It trails each release until the changelog PR merges, which is acceptable.
11. **Footer.** Docs, GitHub, changelog, MIT, and "a Steamfield project".

### Scope
**Must (v1)**
- Sections 1–11. The islands are the Monitor stage, the Queue board, and one shared `TerminalPlayer` that drives both the Commands tabs and the Agent session.
- A docs section that loads `../docs/` (configuration, ssh-setup, troubleshooting, claude-code, MIGRATION). ARCHITECTURE, DEVELOPMENT and releasing are left out because they're contributor docs.
- Every transcript built from real captured rr output (fixtures), plus drift tests.
- Works without JS. Respects reduced motion. Demos are keyboard-operable. axe passes at desktop and mobile widths.
- `llms.txt`, `llms-full.txt`, a `.md` export per docs page, social cards, sitemap, canonical URLs (all included in the stack).
- PostHog with these events:
  - `install_copied {method}`
  - `plugin_copied`
  - `section_viewed {id}`, fired by IntersectionObserver on `#install`, `#queue`, `#commands` and `#agent`
  - `command_tab {name}`, for which commands people look at

**Should**
- A second capture where one host drops offline mid-run, so the stage shows the real offline card ("Offline · retry in Ns") and the task finishing elsewhere.
- A failing-run variant (the `✕` path with a parsed failure in `details.failures`).
- A redirect from `/docs/` to the GitHub docs for pages not included.

**Won't (this version)**
- A light theme. The TUI is dark-only. This means dropping the stack's light theme and running axe on dark only.
- Changelog pages beyond a link. (A blog of how-to articles was added later, for search: it answers the problems rr solves for readers who don't know rr yet.)
- WebGL, sound, CRT effects, easter eggs. Those are Steamfield's identity.
- An interactive shard simulator. It would show scheduling rr doesn't do, and it would duplicate Steamfield's lane demo.
- A GitHub star count or any runtime API call.

### Success criteria
Treat the first two weeks as the baseline. The targets are starting guesses.

| Metric | Starting target | Measurement |
|---|---|---|
| Install copy rate | ≥ 8% of sessions | `install_copied` / sessions |
| Reach of the Install section | ≥ 35% of sessions | `section_viewed{id:"install"}` |
| Downstream installs | Track the trend | GitHub Traffic referrers from `rr.steamfield.io`, release download counts |
| Page weight | ≤ 150 KB of JS on home, LCP < 1.5s on fast 4G | Lighthouse on `make preview` |
| Copy accuracy | 0 mismatches | Drift tests |

### Assumptions and risks
| Assumption | Confidence | Validation |
|---|---|---|
| Most visitors use agents, so the lede and section 3 lead with agents | Med | Section reach and referrers after launch |
| Synthwave can look distinct from steamfield.io | Med | Side-by-side screenshot review before deploy |

| Risk | Mitigation |
|---|---|
| Page copy drifts from the CLI | Drift tests against README, `internal/cli/json.go` and `loadbalance.go`; transcripts from captures |
| PostHog silently drops events | Turn on "Cookieless server hash mode" in the PostHog project: the site runs `cookieless_mode: "on_reject"`, so every visitor who hasn't accepted the consent banner is counted cookieless. Expect an ad-blocker undercount; use the fragment's reverse proxy if the numbers look empty. |
| Neon fails contrast | Token contrast test (below) |
| Frontmatter added to `docs/*.md` looks odd on GitHub | Keep it to `title` and `group`. GitHub renders that as a two-row table. |

## Changes made while building

- The waiting event is `phase: "connect", status: "waiting"` (`roundRobinWait`), not a `lock` phase; the Queue board uses the real event and the same-machine message ("All hosts locked by your own runs (pid N: cmd); waiting up to 1m0s").
- The monitor's usage bars use its threshold colors (cyan, purple, pink), as `RenderGradientBar` does, not the brand gradient.
- The site's fonts have no braille or `▰▱` glyphs, so sparklines and bars are SVG dot grids decoded from the same braille characters the TUI draws. `internal/monitor/site_golden_test.go` and `src/lib/monitor/render.test.ts` pin both sides to `fixtures/monitor-golden.json`.
- The Agent session fixes `TestItoa` (a real test) instead of `TestLockAcquisition`, which doesn't exist; the docs' example name is stale.
- Monitor alerts are opt-in in rr (`monitor.alerts.enabled`); the stage shows them enabled and its caption says so.
