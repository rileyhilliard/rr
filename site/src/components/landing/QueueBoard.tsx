// Section 3: several agents hitting the same hosts. Starts with every host locked
// and a fourth run waiting (the state that answers the question without
// JavaScript); hydrated, you can start runs and finish them to see the handoff.
// Waiters retry on their own 2s clocks from when they started waiting, so the
// one that gets a freed host depends on when you click, as with rr.
import { useState } from "react";
import { POLL_MS } from "../../lib/flow";
import {
  type Agent,
  arrive,
  emptyQueue,
  finish,
  type QueueState,
  waitEvent,
  waitMessage,
} from "../../lib/queue";

const COMMANDS = [
  "go test ./internal/api/...",
  "uv run pytest tests/services -x",
  "bun test src/components",
  "go test ./internal/lock/... -run TestItoa",
  "uv run pytest tests/api -k auth",
  "bun test src/lib",
];
const MAX_AGENTS = 5;

const makeAgent = (id: number): Agent => ({
  id,
  command: COMMANDS[(id - 1) % COMMANDS.length] ?? "",
  pid: 48200 + id * 17,
});

function initial(hosts: string[]): { state: QueueState; next: number } {
  let state = emptyQueue(hosts);
  for (let id = 1; id <= hosts.length + 1; id++) state = arrive(state, makeAgent(id), Date.now());
  return { state, next: hosts.length + 2 };
}

export function QueueBoard({ hosts }: { hosts: string[] }) {
  const [{ state }, setQueue] = useState(() => initial(hosts));
  const active = hosts.filter((host) => state.holders[host]).length + state.waiting.length;
  const firstHolder = hosts.map((host) => state.holders[host]).find(Boolean);

  const start = () =>
    setQueue(({ state, next }) => ({
      state: arrive(state, makeAgent(next), Date.now()),
      next: next + 1,
    }));
  const [changed, setChanged] = useState<string | null>(null);
  const done = (host: string) => {
    setQueue(({ state, next }) => ({ state: finish(state, host, Date.now()), next }));
    setChanged(host);
  };

  if (hosts.length === 0) return null;

  return (
    <div className="term">
      <div className="term-bar justify-between">
        <span>
          {active} {active === 1 ? "run" : "runs"} on {hosts.length} hosts
        </span>
        <button
          type="button"
          onClick={start}
          disabled={active >= MAX_AGENTS}
          className="rounded-control border-line-strong text-ink-2 hover:text-ink border px-2 py-0.5 text-[11px] disabled:opacity-50"
        >
          Start another agent's run
        </button>
      </div>
      <table className="w-full border-collapse text-left text-[12px]">
        <caption className="sr-only">Which agent's run holds each host's lock</caption>
        <thead className="text-muted">
          <tr>
            <th scope="col" className="px-3 py-2 font-normal">
              Host
            </th>
            <th scope="col" className="px-3 py-2 font-normal">
              Lock
            </th>
            <th scope="col" className="px-3 py-2 text-right font-normal">
              <span className="sr-only">Action</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {hosts.map((host) => {
            const holder = state.holders[host];
            return (
              <tr
                // Re-keyed when the holder changes, so the handed-over row flashes once.
                key={`${host}-${holder?.id ?? "free"}`}
                className={`border-line border-t ${changed === host ? "flash" : ""}`}
              >
                <td className="px-3 py-2 whitespace-nowrap">
                  <span aria-hidden="true" className={holder ? "text-waiting" : "text-info"}>
                    {holder ? "◆" : "◉"}
                  </span>{" "}
                  <span className="text-ink">{host}</span>
                </td>
                {/* w-full max-w-0: the lock column takes the leftover width and truncates. */}
                <td className="text-ink-2 w-full max-w-0 px-3 py-2">
                  {holder ? (
                    <>
                      <span className="text-ink">agent {holder.id}</span>{" "}
                      <span className="text-muted">pid {holder.pid}</span>
                      <span className="block truncate">{holder.command}</span>
                    </>
                  ) : (
                    <span className="text-muted">free</span>
                  )}
                </td>
                <td className="px-3 py-2 text-right">
                  {holder && (
                    <button
                      type="button"
                      onClick={() => done(host)}
                      className="rounded-control border-line-strong text-ink-2 hover:text-ink border px-2 py-0.5 text-[11px] whitespace-nowrap"
                    >
                      Finish run<span className="sr-only"> on {host}</span>
                    </button>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <div className="border-line border-t px-3 py-3" aria-live="polite">
        {state.waiting.length > 0 && firstHolder ? (
          <>
            <p className="text-ink-2 text-[12px]">
              <span className="text-ink">
                agent {state.waiting.map((agent) => agent.id).join(", agent ")}
              </span>{" "}
              waiting, retrying every {POLL_MS / 1000}s. A freed host goes to whichever run retries
              first, which may not be the one that arrived first. With <code>--pretty</code>:
            </p>
            <pre className="text-waiting mt-1 overflow-x-auto text-[12px] whitespace-pre-wrap">
              ⣾ {waitMessage(firstHolder)}
            </pre>
            <p className="text-ink-2 mt-2 text-[12px]">And the event an agent reads on stderr:</p>
            <pre className="text-ink-2 mt-1 text-[12px] break-all whitespace-pre-wrap">
              {waitEvent(firstHolder)}
            </pre>
          </>
        ) : (
          <p className="text-muted text-[12px]">
            Nobody waiting. A new run takes the first free host in <code>hosts:</code> order.
          </p>
        )}
      </div>
    </div>
  );
}
