// @vitest-environment node
import { describe, expect, it } from "vitest";
import { POLL_MS } from "./flow";
import {
  type Agent,
  arrive,
  emptyQueue,
  finish,
  freeHost,
  nextRetryIn,
  waitEvent,
  waitMessage,
} from "./queue";

const agent = (id: number): Agent => ({ id, command: `go test ./pkg${id}/...`, pid: 40000 + id });
const hosts = ["m4-mini", "m1-linux", "m1-mini"];

describe("the queue follows rr's host choice", () => {
  it("gives each new run the next unlocked host in config order", () => {
    let state = emptyQueue(hosts);
    state = arrive(state, agent(1));
    state = arrive(state, agent(2));
    expect(state.holders["m4-mini"]?.id).toBe(1);
    expect(state.holders["m1-linux"]?.id).toBe(2);
    expect(freeHost(state)).toBe("m1-mini");
  });

  it("makes a run wait when every host is locked, then hands it a host that frees", () => {
    let state = emptyQueue(hosts);
    for (const id of [1, 2, 3, 4]) state = arrive(state, agent(id));
    expect(state.waiting.map((a) => a.id)).toEqual([4]);
    expect(freeHost(state)).toBeUndefined();
    state = finish(state, "m1-linux");
    expect(state.holders["m1-linux"]?.id).toBe(4);
    expect(state.waiting).toEqual([]);
  });

  it("hands a freed host to the first waiter to retry, not the longest waiter", () => {
    let state = emptyQueue(hosts);
    for (const id of [1, 2, 3]) state = arrive(state, agent(id), 0);
    state = arrive(state, agent(4), 0);
    state = arrive(state, agent(5), 1500);
    // At 2600, agent 4 next retries at 4000 and agent 5 at 3500.
    expect(state.waiting.map((waiter) => nextRetryIn(waiter, 2600))).toEqual([1400, 900]);
    state = finish(state, "m4-mini", 2600);
    expect(state.holders["m4-mini"]?.id).toBe(5);
    expect(state.holders["m4-mini"]).not.toHaveProperty("since");
    expect(state.waiting.map((a) => a.id)).toEqual([4]);
  });

  it("retries every POLL_MS from when the run started waiting", () => {
    const waiter = { ...agent(1), since: 300 };
    expect(nextRetryIn(waiter, 300)).toBe(0);
    expect(nextRetryIn(waiter, 300 + POLL_MS * 3 + 500)).toBe(POLL_MS - 500);
  });

  it("frees a host when nobody is waiting", () => {
    let state = arrive(emptyQueue(hosts), agent(1));
    state = finish(state, "m4-mini");
    expect(freeHost(state)).toBe("m4-mini");
  });

  it("says what it waits on, as rr does for runs on this machine", () => {
    expect(waitMessage(agent(1))).toBe(
      "All hosts locked by your own runs (pid 40001: go test ./pkg1/...); waiting up to 1m0s",
    );
    expect(JSON.parse(waitEvent(agent(1)))).toMatchObject({ phase: "connect", status: "waiting" });
  });
});
