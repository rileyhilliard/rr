// The Queue board's model of rr's host choice (findAvailableHost and
// roundRobinWait in internal/cli/loadbalance.go): a run takes the first unlocked
// host in `hosts:` order; when every host is locked it waits, retrying every
// POLL_MS. Nothing orders the waiters, so a freed host goes to whichever retries
// first, not the one waiting longest. content-drift.test.ts checks the messages
// and the retry interval against the Go.
import { POLL_MS } from "./flow";

export type Agent = { id: number; command: string; pid: number };
/** A waiting run and when it started waiting (ms), which sets its retry times. */
export type Waiter = Agent & { since: number };
export type QueueState = {
  hosts: string[];
  /** Host name → the agent holding its lock. */
  holders: Record<string, Agent | undefined>;
  /** Agents waiting for any host, in arrival order (which decides nothing). */
  waiting: Waiter[];
};

export const WAIT_TIMEOUT = "1m0s";

export function emptyQueue(hosts: string[]): QueueState {
  return { hosts, holders: {}, waiting: [] };
}

/** The first unlocked host in config order, or undefined when all are locked. */
export function freeHost(state: QueueState): string | undefined {
  return state.hosts.find((host) => !state.holders[host]);
}

/** A new run at time `now`: lock the first free host, or join the wait. */
export function arrive(state: QueueState, agent: Agent, now = 0): QueueState {
  const host = freeHost(state);
  if (host) return { ...state, holders: { ...state.holders, [host]: agent } };
  return { ...state, waiting: [...state.waiting, { ...agent, since: now }] };
}

/** How long after `now` this waiter's next retry comes. */
export function nextRetryIn(waiter: Waiter, now: number): number {
  const sinceLast = (now - waiter.since) % POLL_MS;
  return sinceLast === 0 ? 0 : POLL_MS - sinceLast;
}

/** A run finishes at `now`: its lock is released and the first waiter to retry takes the host. */
export function finish(state: QueueState, host: string, now = 0): QueueState {
  const next = state.waiting.reduce<Waiter | undefined>(
    (first, waiter) =>
      first && nextRetryIn(first, now) <= nextRetryIn(waiter, now) ? first : waiter,
    undefined,
  );
  if (!next) return { ...state, holders: { ...state.holders, [host]: undefined } };
  const { since: _since, ...agent } = next;
  return {
    ...state,
    holders: { ...state.holders, [host]: agent },
    waiting: state.waiting.filter((waiter) => waiter !== next),
  };
}

/** waitMessage for runs on this machine: the holder rr names is one of yours. */
export function waitMessage(holder: Agent): string {
  return `All hosts locked by your own runs (pid ${holder.pid}: ${holder.command}); waiting up to ${WAIT_TIMEOUT}`;
}

/** The JSON event a waiting run writes to stderr (roundRobinWait), trimmed of `holders`. */
export function waitEvent(holder: Agent): string {
  return JSON.stringify({
    type: "phase",
    phase: "connect",
    status: "waiting",
    details: { message: waitMessage(holder), wait_timeout_s: 60 },
  });
}
