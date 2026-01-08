package ui

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSpinner(t *testing.T) {
	s := NewSpinner("Testing")
	assert.Equal(t, "Testing", s.Label())
	assert.Equal(t, SpinnerPending, s.State())
}

func TestSpinnerStartStop(t *testing.T) {
	var buf strings.Builder
	var mu sync.Mutex

	s := NewSpinner("Test")
	s.SetOutput(func(str string) {
		mu.Lock()
		buf.WriteString(str)
		mu.Unlock()
	})

	s.Start()
	assert.Equal(t, SpinnerInProgress, s.State())

	// Let it spin a bit
	time.Sleep(50 * time.Millisecond)

	s.Stop()

	// After stop, state should still be in-progress (not changed by Stop)
	assert.Equal(t, SpinnerInProgress, s.State())
}

func TestSpinnerSuccess(t *testing.T) {
	var buf strings.Builder
	var mu sync.Mutex

	s := NewSpinner("Test")
	s.SetOutput(func(str string) {
		mu.Lock()
		buf.WriteString(str)
		mu.Unlock()
	})

	s.Start()
	time.Sleep(20 * time.Millisecond)
	s.Success()

	assert.Equal(t, SpinnerSuccess, s.State())

	mu.Lock()
	output := buf.String()
	mu.Unlock()

	// Should contain the success symbol
	assert.Contains(t, output, SymbolComplete)
}

func TestSpinnerFail(t *testing.T) {
	var buf strings.Builder
	var mu sync.Mutex

	s := NewSpinner("Test")
	s.SetOutput(func(str string) {
		mu.Lock()
		buf.WriteString(str)
		mu.Unlock()
	})

	s.Start()
	time.Sleep(20 * time.Millisecond)
	s.Fail()

	assert.Equal(t, SpinnerFailed, s.State())

	mu.Lock()
	output := buf.String()
	mu.Unlock()

	// Should contain the fail symbol
	assert.Contains(t, output, SymbolFail)
}

func TestSpinnerSkip(t *testing.T) {
	var buf strings.Builder
	var mu sync.Mutex

	s := NewSpinner("Test")
	s.SetOutput(func(str string) {
		mu.Lock()
		buf.WriteString(str)
		mu.Unlock()
	})

	s.Start()
	time.Sleep(20 * time.Millisecond)
	s.Skip()

	assert.Equal(t, SpinnerSkipped, s.State())

	mu.Lock()
	output := buf.String()
	mu.Unlock()

	// Should contain the skipped symbol
	assert.Contains(t, output, SymbolSkipped)
}

func TestSpinnerElapsed(t *testing.T) {
	s := NewSpinner("Test")
	s.SetOutput(func(_ string) {})

	// Before start, elapsed should be 0
	assert.Equal(t, time.Duration(0), s.Elapsed())

	s.Start()
	time.Sleep(50 * time.Millisecond)
	s.Stop()

	// After running, elapsed should be > 0
	assert.Greater(t, s.Elapsed(), time.Duration(0))
}

func TestSpinnerSetLabel(t *testing.T) {
	s := NewSpinner("Initial")
	assert.Equal(t, "Initial", s.Label())

	s.SetLabel("Updated")
	assert.Equal(t, "Updated", s.Label())
}

func TestSpinnerDoubleStart(t *testing.T) {
	s := NewSpinner("Test")
	s.SetOutput(func(_ string) {})

	s.Start()
	s.Start() // Second start should be no-op

	assert.Equal(t, SpinnerInProgress, s.State())
	s.Stop()
}

func TestSpinnerDoubleStop(t *testing.T) {
	s := NewSpinner("Test")
	s.SetOutput(func(_ string) {})

	s.Start()
	s.Stop()
	s.Stop() // Second stop should be no-op

	// Should not panic
	assert.Equal(t, SpinnerInProgress, s.State())
}

func TestSpinnerFrames(t *testing.T) {
	// Verify spinner frames are the expected Unicode characters
	expected := []string{"◐", "◓", "◑", "◒"}
	assert.Equal(t, expected, spinnerFrames)
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		want     string
	}{
		{0, "0.00s"},
		{50 * time.Millisecond, "0.05s"},
		{100 * time.Millisecond, "0.1s"},
		{1 * time.Second, "1.0s"},
		{1500 * time.Millisecond, "1.5s"},
		{10 * time.Second, "10.0s"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatDuration(tt.duration)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSpinnerConcurrentAccess(t *testing.T) {
	s := NewSpinner("Test")
	s.SetOutput(func(_ string) {})

	s.Start()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.State()
			_ = s.Label()
			_ = s.Elapsed()
		}()
	}

	wg.Wait()
	s.Success()

	require.Equal(t, SpinnerSuccess, s.State())
}
