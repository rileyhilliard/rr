// Browser fakes shared by the component and page-script tests. happy-dom never reports
// intersection and its matchMedia doesn't follow a preference, so tests drive both here.
import { vi } from "vitest";

type Observed = { callback: IntersectionObserverCallback; targets: Set<Element> };

/**
 * Replaces IntersectionObserver with a fake that reports only when told. `intersect`
 * tells every observer watching `target` that it entered (or left) the viewport.
 */
export function stubIntersectionObserver() {
  const observers: Observed[] = [];
  class FakeObserver {
    readonly entry: Observed;
    constructor(callback: IntersectionObserverCallback) {
      this.entry = { callback, targets: new Set() };
      observers.push(this.entry);
    }
    observe(target: Element) {
      this.entry.targets.add(target);
    }
    unobserve(target: Element) {
      this.entry.targets.delete(target);
    }
    disconnect() {
      this.entry.targets.clear();
    }
    takeRecords() {
      return [];
    }
  }
  vi.stubGlobal("IntersectionObserver", FakeObserver);
  return {
    intersect(target: Element, isIntersecting = true) {
      for (const observer of observers) {
        if (!observer.targets.has(target)) continue;
        observer.callback(
          [{ target, isIntersecting } as IntersectionObserverEntry],
          observer as unknown as IntersectionObserver,
        );
      }
    },
  };
}

/**
 * Fakes `prefers-reduced-motion`, keeping each query's change listeners so `set` can flip
 * the preference live, as a visitor changing their OS setting would.
 */
export function stubReducedMotion(initial: boolean) {
  let reduce = initial;
  const queries: { query: string; listeners: Set<() => void> }[] = [];
  vi.spyOn(window, "matchMedia").mockImplementation((query: string) => {
    const entry = { query, listeners: new Set<() => void>() };
    queries.push(entry);
    return {
      get matches() {
        return reduce && query.includes("reduce");
      },
      media: query,
      addEventListener: (_: string, listener: () => void) => entry.listeners.add(listener),
      removeEventListener: (_: string, listener: () => void) => entry.listeners.delete(listener),
      addListener: (listener: () => void) => entry.listeners.add(listener),
      removeListener: (listener: () => void) => entry.listeners.delete(listener),
    } as unknown as MediaQueryList;
  });
  return {
    set(next: boolean) {
      reduce = next;
      for (const { listeners } of queries) for (const listener of [...listeners]) listener();
    },
  };
}

/** Hides or shows the tab, firing visibilitychange as a browser does. */
export function setTabHidden(hidden: boolean) {
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => (hidden ? "hidden" : "visible"),
  });
  Object.defineProperty(document, "hidden", { configurable: true, get: () => hidden });
  document.dispatchEvent(new Event("visibilitychange"));
}
