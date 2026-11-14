// @vitest-environment node
import { describe, expect, it } from "vitest";
import golden from "../../../fixtures/monitor-golden.json";
import {
  alertFiring,
  formatBytes,
  formatLockDuration,
  formatPercent,
  formatRate,
  gradientBar,
  heatStrip,
  latencyBand,
  resample,
  severity,
  sparkline,
  THRESHOLDS,
  truncate,
} from "./render";

// fixtures/monitor-golden.json is the Go TUI's own output for these inputs
// (internal/monitor/site_golden_test.go writes and checks it).
describe("the monitor port matches the TUI", () => {
  it.each(golden.sparklines)("sparkline $width×$height of $data.length points", (c) => {
    expect(sparkline(c.data, c.width, c.height, c.force_zero_min).rows).toEqual(c.rows);
  });

  it.each(golden.bars)("gradient bar $width at $percent%", (c) => {
    expect(gradientBar(c.width, c.percent)).toBe(c.text);
  });

  it("caps the core heat strip", () => {
    expect(heatStrip(Array(12).fill(50), golden.heat_strip.input)).toBe(golden.heat_strip.text);
  });

  it.each(golden.rates)("formats $input B/s", (c) => {
    expect(formatRate(c.input)).toBe(c.text);
  });

  it.each(golden.bytes)("formats $input bytes", (c) => {
    expect(formatBytes(c.input)).toBe(c.text);
  });

  it.each(golden.durations_ms)("formats a lock held $input ms", (c) => {
    expect(formatLockDuration(c.input)).toBe(c.text);
  });

  it.each(golden.percents)("formats $input%", (c) => {
    expect(formatPercent(c.input)).toBe(c.text);
  });

  it.each(golden.resample)("resamples $data.length points to $target", (c) => {
    const result = resample(c.data, c.target);
    expect(result).toHaveLength(c.result.length);
    for (const [i, value] of result.entries()) {
      expect(value).toBeCloseTo(c.result[i] ?? Number.NaN, 9);
    }
  });
});

describe("monitor rules", () => {
  it("colors by the default thresholds", () => {
    expect(severity(69.9, THRESHOLDS.cpu)).toBe("healthy");
    expect(severity(70, THRESHOLDS.cpu)).toBe("warning");
    expect(severity(90, THRESHOLDS.cpu)).toBe("critical");
    expect(severity(94, THRESHOLDS.disk)).toBe("warning");
  });

  it("bands latency like LatencyQualityText", () => {
    expect([10, 50, 200, 500].map(latencyBand)).toEqual(["fast", "normal", "slow", "degraded"]);
  });

  it("keeps an alert firing until the metric drops below warning", () => {
    expect(alertFiring([50, 92, 80, 95, 60, 91], THRESHOLDS.cpu)).toEqual([
      false,
      true,
      true,
      true,
      false,
      true,
    ]);
  });

  it("truncates commands like the card", () => {
    expect(truncate("uv run pytest tests/services", 12)).toBe("uv run py...");
  });
});
