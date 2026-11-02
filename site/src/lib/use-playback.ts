// Shared clock for the landing page demos (Monitor stage, terminal player). It
// advances only while the demo is on screen, the tab is visible, motion is allowed
// and the visitor hasn't paused it. Before hydration and under reduced motion it
// reports `still`, and each demo renders its finished or representative frame.
import { type RefObject, useCallback, useEffect, useRef, useState } from "react";

const TICK_MS = 50;

export type Playback = {
  ref: RefObject<HTMLDivElement | null>;
  /** Milliseconds of demo time elapsed, capped at duration. */
  elapsed: number;
  playing: boolean;
  paused: boolean;
  /** No motion: not hydrated yet, or the visitor prefers reduced motion. */
  still: boolean;
  complete: boolean;
  toggle: () => void;
  replay: () => void;
  /** Jump to a moment and keep playing from there. */
  seek: (ms: number) => void;
};

/** `smooth` advances on animation frames instead of 50ms ticks, for moving shapes. */
export function usePlayback(duration: number, speed = 1, smooth = false): Playback {
  const ref = useRef<HTMLDivElement>(null);
  const [ready, setReady] = useState(false);
  const [reduced, setReduced] = useState(false);
  const [visible, setVisible] = useState(false);
  const [hidden, setHidden] = useState(false);
  const [paused, setPaused] = useState(false);
  const [elapsed, setElapsed] = useState(0);

  useEffect(() => {
    const motion = window.matchMedia("(prefers-reduced-motion: reduce)");
    const onMotion = () => setReduced(motion.matches);
    const onVisibility = () => setHidden(document.visibilityState === "hidden");
    onMotion();
    onVisibility();
    setReady(true);
    motion.addEventListener("change", onMotion);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      motion.removeEventListener("change", onMotion);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, []);

  useEffect(() => {
    const node = ref.current;
    if (!node || typeof IntersectionObserver === "undefined") {
      setVisible(true);
      return;
    }
    const observer = new IntersectionObserver(
      ([entry]) => setVisible(entry?.isIntersecting ?? false),
      {
        threshold: 0.15,
      },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  const still = !ready || reduced;
  const complete = elapsed >= duration;
  const playing = !still && visible && !hidden && !paused && !complete;

  useEffect(() => {
    if (!playing || !smooth) return;
    let last = performance.now();
    let frame = requestAnimationFrame(function step(now) {
      const delta = Math.min(now - last, 100) * speed;
      last = now;
      setElapsed((time) => Math.min(time + delta, duration));
      frame = requestAnimationFrame(step);
    });
    return () => cancelAnimationFrame(frame);
  }, [playing, smooth, duration, speed]);

  useEffect(() => {
    if (!playing || smooth) return;
    const timer = window.setInterval(
      () => setElapsed((time) => Math.min(time + TICK_MS * speed, duration)),
      TICK_MS,
    );
    return () => window.clearInterval(timer);
  }, [playing, smooth, duration, speed]);

  const toggle = useCallback(() => setPaused((value) => !value), []);
  const replay = useCallback(() => {
    setElapsed(0);
    setPaused(false);
  }, []);

  const seek = useCallback(
    (ms: number) => {
      setElapsed(Math.max(0, Math.min(ms, duration)));
      setPaused(false);
    },
    [duration],
  );

  return { ref, elapsed, playing, paused, still, complete, toggle, replay, seek };
}

/** A frame counter on its own clock (the TUI's spinner ticks every 150ms). */
export function useTicker(active: boolean, intervalMs = 150): number {
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (!active) return;
    const timer = window.setInterval(() => setTick((value) => value + 1), intervalMs);
    return () => window.clearInterval(timer);
  }, [active, intervalMs]);
  return tick;
}
