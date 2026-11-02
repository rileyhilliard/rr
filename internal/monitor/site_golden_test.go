package monitor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The landing page (site/) redraws monitor cards in TypeScript. This test pins the
// plain text the TUI renders for fixed inputs in site/fixtures/monitor-golden.json,
// and the site's render.test.ts checks its port against the same file, so a change
// on either side fails a test. Regenerate after an intended rendering change:
//
//	RR_WRITE_SITE_GOLDEN=1 go test ./internal/monitor -run TestSiteGolden

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansiPattern.ReplaceAllString(s, "") }

type goldenSparkline struct {
	Data         []float64 `json:"data"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	ForceZeroMin bool      `json:"force_zero_min"`
	Rows         []string  `json:"rows"`
}

type goldenBar struct {
	Width   int     `json:"width"`
	Percent float64 `json:"percent"`
	Text    string  `json:"text"`
}

type goldenFormat struct {
	Input float64 `json:"input"`
	Text  string  `json:"text"`
}

type siteGolden struct {
	Sparklines []goldenSparkline `json:"sparklines"`
	Bars       []goldenBar       `json:"bars"`
	HeatStrip  goldenFormat      `json:"heat_strip"`
	Rates      []goldenFormat    `json:"rates"`
	Bytes      []goldenFormat    `json:"bytes"`
	Durations  []goldenFormat    `json:"durations_ms"`
	Percents   []goldenFormat    `json:"percents"`
	Resample   []goldenResample  `json:"resample"`
}

type goldenResample struct {
	Data   []float64 `json:"data"`
	Target int       `json:"target"`
	Result []float64 `json:"result"`
}

func buildSiteGolden() siteGolden {
	ramp := make([]float64, 0, 40)
	for i := 0; i < 40; i++ {
		ramp = append(ramp, float64(i)*2.5)
	}
	spiky := []float64{12, 15, 14, 88, 93, 97, 91, 40, 22, 18, 76, 81, 95, 99, 30}
	latency := []float64{12, 14, 11, 260, 240, 30, 18, 640, 22, 15}

	g := siteGolden{}
	for _, c := range []struct {
		data      []float64
		w, h      int
		forceZero bool
	}{
		{ramp, 20, 2, false},
		{ramp, 8, 4, false},
		{spiky, 20, 2, false},
		{spiky, 4, 2, false},
		{[]float64{50}, 6, 2, false},
		{latency, 12, 2, true},
		{SmoothWithMovingAverage(latency, 5), 12, 2, true},
	} {
		rows := strings.Split(plain(RenderBrailleSparklineWithOptions(c.data, c.w, c.h, ColorGraph, nil, c.forceZero)), "\n")
		g.Sparklines = append(g.Sparklines, goldenSparkline{Data: c.data, Width: c.w, Height: c.h, ForceZeroMin: c.forceZero, Rows: rows})
	}
	for _, c := range []struct {
		w int
		p float64
	}{{10, 0}, {10, 37}, {20, 71.5}, {12, 100}, {1, 50}} {
		g.Bars = append(g.Bars, goldenBar{Width: c.w, Percent: c.p, Text: plain(RenderGradientBar(c.w, c.p, ColorGraph))})
	}
	g.HeatStrip = goldenFormat{Input: 10, Text: plain(RenderCoreHeatStrip([]float64{1, 20, 40, 60, 75, 80, 91, 99, 5, 50, 70, 3}, 10, nil))}
	for _, v := range []float64{0, 900, 2463, 1048575, 5 * 1024 * 1024, 3.2 * 1024 * 1024 * 1024} {
		g.Rates = append(g.Rates, goldenFormat{Input: v, Text: FormatRate(v)})
	}
	for _, v := range []int64{512, 18448404480, 245107195904, 8589934592} {
		g.Bytes = append(g.Bytes, goldenFormat{Input: float64(v), Text: formatBytes(v)})
	}
	for _, ms := range []float64{400, 41_500, 120_500, 150_500} {
		info := HostLockInfo{Started: time.Now().Add(-time.Duration(ms) * time.Millisecond)}
		g.Durations = append(g.Durations, goldenFormat{Input: ms, Text: info.FormatDuration()})
	}
	for _, v := range []float64{0, 7.25, 7.35, 16.15, 61.05, 99.95, 100} {
		g.Percents = append(g.Percents, goldenFormat{Input: v, Text: fmt.Sprintf("%5.1f%%", v)})
	}
	for _, c := range []struct {
		data   []float64
		target int
	}{{spiky, 5}, {[]float64{0, 10}, 5}, {ramp, 7}} {
		g.Resample = append(g.Resample, goldenResample{Data: c.data, Target: c.target, Result: resampleData(c.data, c.target)})
	}
	return g
}

func TestSiteGolden(t *testing.T) {
	path := filepath.Join("..", "..", "site", "fixtures", "monitor-golden.json")
	got, err := json.MarshalIndent(buildSiteGolden(), "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')

	if os.Getenv("RR_WRITE_SITE_GOLDEN") == "1" {
		require.NoError(t, os.WriteFile(path, got, 0o644))
		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "site golden missing; run RR_WRITE_SITE_GOLDEN=1 go test ./internal/monitor -run TestSiteGolden")
	require.JSONEq(t, string(want), string(got),
		"monitor rendering changed; regenerate with RR_WRITE_SITE_GOLDEN=1 go test ./internal/monitor -run TestSiteGolden, then make the site's src/lib/monitor/render.ts match")
}
