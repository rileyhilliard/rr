// Public product metadata. Every landing section reads from here; a section whose
// value is empty (null or an empty list) doesn't render. The copy comes from the
// repository's README, and src/content-drift.test.ts checks the commands and
// codes against the README and the Go source.
import { readFileSync } from "node:fs";
import { join } from "node:path";

export type Link = { label: string; href: string };
export type InstallOption = { label: string; command: string };
export type Row = string[];

const repo = "https://github.com/rileyhilliard/rr";

/** The newest released version: the first `## [x.y.z]` heading in the repo's CHANGELOG. */
function changelogVersion(): string {
  try {
    // Builds and tests run from site/; import.meta.url would point into dist/ once bundled.
    const changelog = readFileSync(join(process.cwd(), "..", "CHANGELOG.md"), "utf8");
    return /^## \[(\d+\.\d+\.\d+)\]/m.exec(changelog)?.[1] ?? "";
  } catch {
    // Built outside the rr repository (no CHANGELOG beside it): hide the version.
    return "";
  }
}

export const site = {
  name: "rr",
  tagline: "Load-balance your coding agents' test runs across your own machines.",
  description:
    "rr syncs your working tree to a free machine over SSH, locks it, runs your tests there and streams the output back. Several coding agents can test at once without slowing down your laptop.",
  home: {
    title: "Remote Test Runner for Coding Agents",
    description:
      "rr is an open-source remote test runner for coding agents. Send tests to your own machines over SSH, queue concurrent runs, and run shards across hosts.",
    lede: "rr is a remote test runner. It locks a free machine, syncs your working tree over SSH, and streams the test output back. Several coding agents can test at once without slowing down your laptop.",
  },
  /** Absolute production URL: canonicals, the sitemap, OG images and llms.txt use it. */
  url: "https://rr.steamfield.io",
  repo,
  /** Current release, shown in the header. Empty hides it. */
  version: changelogVersion(),
  announcement: null as Link | null,
  install: [
    { label: "Homebrew", command: "brew install rileyhilliard/tap/rr" },
    {
      label: "Script",
      command:
        "curl -sSL https://raw.githubusercontent.com/rileyhilliard/rr/main/scripts/install.sh | bash",
    },
    { label: "Go", command: "go install github.com/rileyhilliard/rr/cmd/rr@latest" },
  ] as InstallOption[],
  /** Claude Code users add the plugin after installing rr; it teaches Claude the CLI. */
  plugin: [
    "/plugin marketplace add https://github.com/rileyhilliard/rr",
    "/plugin install rr@rr",
    "/rr:setup",
  ],
  /** The "why": README "Why rr", as the same day with and without rr. */
  why: {
    before: {
      title: "Agents test on your laptop",
      lines: [
        "Every agent runs the suite after every change, and each run pins every core for minutes.",
        "Two agents in two worktrees start their suites at once and fight over CPU, ports and the test database.",
        "Runs that collide fail even when the code is fine, and the laptop stops responding.",
      ],
    },
    after: {
      title: "Agents test on spare machines",
      lines: [
        "Each run locks a free host, syncs the tree, runs there and streams the output back.",
        "The next agent takes the next free host, or waits its turn and says who it's waiting on.",
        "No two runs share a machine, and your laptop stays free for the editor and the agents.",
      ],
    },
  },
  /** README "Parallel execution": the three-task example and its timings. */
  parallel: {
    caveat:
      "rr hands tasks out in the order you list them, and each host takes the next one as soon as it's free. List the long ones first, and keep shards similar in size. One command still runs on one host, so the pooling comes from how you split the work.",
    command: "rr --pretty test",
    output: [
      "Parallel Execution Summary",
      "",
      "  ◉ test-backend on m1-linux (52.9s)",
      "  ◉ test-frontend on m1-mini (4.0s)",
      "  ◉ test-pipeline on m4-mini (40.0s)",
      "",
      "  ◉ 3 passed  ✕ 0 failed  ● 3 total  (52.9s)",
      "  Hosts: m1-linux, m1-mini, m4-mini",
    ],
    sequentialSeconds: 97,
    parallelSeconds: 52.9,
    tasks: [
      { name: "test-backend", host: "m1-linux", seconds: 52.9 },
      { name: "test-pipeline", host: "m4-mini", seconds: 40.0 },
      { name: "test-frontend", host: "m1-mini", seconds: 4.0 },
    ],
  },
  /** README "How it works": one host, three SSH routes, tried in parallel. */
  fallback: {
    config: `hosts:
  gpu-box:
    ssh:
      - 192.168.1.50    # LAN, fastest
      - gpu-tailscale   # VPN, works from anywhere
      - user@backup-gpu # backup machine`,
    rows: [
      ["Home (LAN)", "Uses 192.168.1.50"],
      ["Coffee shop", "LAN unreachable, uses Tailscale"],
      ["gpu-box down", "Falls through to backup-gpu"],
    ] as Row[],
  },
  /** Codes from internal/cli/json.go that an agent is most likely to branch on. */
  errorCodes: [
    ["LOCK_HELD", "Every host is busy and the wait ran out"],
    ["SSH_TIMEOUT", "No route to the host answered in time"],
    ["SSH_AUTH_FAILED", "The host refused the key"],
    ["DEPENDENCY_MISSING", "A tool in require: isn't on the host"],
    ["HOST_NOT_FOUND", "The config names a host that doesn't exist"],
    ["CONFIG_NOT_FOUND", "No .rr.yaml; run rr init"],
  ] as Row[],
  setup: {
    global: `# ~/.rr/config.yaml: your machines, not committed
version: 1
hosts:
  mini:
    ssh:
      - mac-mini.local      # try LAN first
      - mac-mini-tailscale  # fall back to VPN
    dir: \${HOME}/projects/\${PROJECT}
    require: [go, node]`,
    project: `# .rr.yaml: shared with your team
version: 1
hosts: [mini]
local_fallback: false   # never quietly run on the laptop

tasks:
  test:
    run: go test {args:-./...}`,
  },
  compare: [
    ["rsync && ssh", "Works for one host, with no locking, failover or splitting across hosts"],
    [
      "CI (GitHub Actions)",
      "Needs a push per iteration and queues for minutes. Too slow for an inner loop",
    ],
    ["Ansible", "Needs an inventory file and a playbook to run one command"],
    [
      "DevPod / Tilt",
      "Built around containers, which is more than syncing and running a command needs",
    ],
    ["VS Code Remote", "Runs inside the editor, so a CLI agent can't use it"],
  ] as Row[],
  requirements: [
    "ssh and rsync on your machine and on each host",
    "Passwordless SSH to each host",
    "Your project's toolchain on the hosts (require: and rr provision check it)",
    "macOS or Linux; Windows under WSL",
  ],
  social: [{ label: "GitHub", href: repo }] as Link[],
  releasesUrl: `${repo}/releases`,
  license: "MIT",
  /** Components that hydrate on the landing page. The build test fails on any other island. */
  islands: [
    "MonitorStage",
    "FlowDiagram",
    "QueueBoard",
    "CommandTabs",
    "TerminalPlayer",
    "InstallTabs",
  ],
};

/** Docs sidebar groups, in order. A page's `group` frontmatter must be one of these ids. */
export const docsGroups = [
  { id: "getting-started", label: "Getting started" },
  { id: "guides", label: "Guides" },
  { id: "reference", label: "Reference" },
] as const;

export type DocsGroupId = (typeof docsGroups)[number]["id"];

if (!["https:", "http:"].includes(new URL(site.url).protocol)) {
  throw new Error("Set site.url to an absolute HTTP or HTTPS production URL.");
}
