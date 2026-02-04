package dashboard

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rileyhilliard/rr/internal/parallel"
	"github.com/stretchr/testify/assert"
)

func TestNewModel(t *testing.T) {
	cancel := func() {}
	m := NewModel(5, cancel)

	assert.Equal(t, 0, len(m.tasks))
	assert.Equal(t, 5, cap(m.tasks))
	assert.Equal(t, 0, m.selected)
	assert.False(t, m.completed)
	assert.Equal(t, 0, m.passed)
	assert.Equal(t, 0, m.failed)
	assert.NotNil(t, m.cancelFunc)
}

func TestModel_InitTasks(t *testing.T) {
	m := NewModel(3, nil)

	initMsg := InitTasksMsg{
		Tasks: []parallel.TaskInit{
			{Name: "task1", Index: 0},
			{Name: "task2", Index: 1},
			{Name: "task3", Index: 2},
		},
	}

	newModel, _ := m.Update(initMsg)
	m = newModel.(Model)

	assert.Len(t, m.tasks, 3)
	assert.Equal(t, "task1", m.tasks[0].Name)
	assert.Equal(t, "task2", m.tasks[1].Name)
	assert.Equal(t, "task3", m.tasks[2].Name)

	for _, task := range m.tasks {
		assert.Equal(t, parallel.TaskPending, task.Status)
	}
}

func TestModel_TaskSyncing(t *testing.T) {
	m := NewModel(2, nil)

	// Initialize tasks
	initMsg := InitTasksMsg{
		Tasks: []parallel.TaskInit{
			{Name: "task1", Index: 0},
			{Name: "task2", Index: 1},
		},
	}
	newModel, _ := m.Update(initMsg)
	m = newModel.(Model)

	// Sync task 0
	syncMsg := TaskSyncingMsg{Name: "task1", Index: 0, Host: "host-a"}
	newModel, _ = m.Update(syncMsg)
	m = newModel.(Model)

	assert.Equal(t, parallel.TaskSyncing, m.tasks[0].Status)
	assert.Equal(t, "host-a", m.tasks[0].Host)
	assert.False(t, m.tasks[0].StartTime.IsZero())
	assert.Equal(t, parallel.TaskPending, m.tasks[1].Status)
}

func TestModel_TaskExecuting(t *testing.T) {
	m := NewModel(1, nil)

	// Initialize and sync
	initMsg := InitTasksMsg{Tasks: []parallel.TaskInit{{Name: "task1", Index: 0}}}
	newModel, _ := m.Update(initMsg)
	m = newModel.(Model)

	syncMsg := TaskSyncingMsg{Name: "task1", Index: 0, Host: "host-a"}
	newModel, _ = m.Update(syncMsg)
	m = newModel.(Model)

	// Execute
	execMsg := TaskExecutingMsg{Name: "task1", Index: 0}
	newModel, _ = m.Update(execMsg)
	m = newModel.(Model)

	assert.Equal(t, parallel.TaskRunning, m.tasks[0].Status)
}

func TestModel_TaskCompleted_Success(t *testing.T) {
	m := NewModel(1, nil)

	// Initialize
	initMsg := InitTasksMsg{Tasks: []parallel.TaskInit{{Name: "task1", Index: 0}}}
	newModel, _ := m.Update(initMsg)
	m = newModel.(Model)

	// Complete successfully
	completedMsg := TaskCompletedMsg{
		Name:     "task1",
		Index:    0,
		Success:  true,
		Duration: 2 * time.Second,
	}
	newModel, _ = m.Update(completedMsg)
	m = newModel.(Model)

	assert.Equal(t, parallel.TaskPassed, m.tasks[0].Status)
	assert.Equal(t, 2*time.Second, m.tasks[0].Duration)
	assert.Equal(t, 1, m.passed)
	assert.Equal(t, 0, m.failed)
}

func TestModel_TaskCompleted_Failure(t *testing.T) {
	m := NewModel(1, nil)

	// Initialize
	initMsg := InitTasksMsg{Tasks: []parallel.TaskInit{{Name: "task1", Index: 0}}}
	newModel, _ := m.Update(initMsg)
	m = newModel.(Model)

	// Complete with failure
	completedMsg := TaskCompletedMsg{
		Name:     "task1",
		Index:    0,
		Success:  false,
		Duration: 1 * time.Second,
	}
	newModel, _ = m.Update(completedMsg)
	m = newModel.(Model)

	assert.Equal(t, parallel.TaskFailed, m.tasks[0].Status)
	assert.Equal(t, 1*time.Second, m.tasks[0].Duration)
	assert.Equal(t, 0, m.passed)
	assert.Equal(t, 1, m.failed)
}

func TestModel_TaskRequeued(t *testing.T) {
	m := NewModel(1, nil)

	// Initialize and sync
	initMsg := InitTasksMsg{Tasks: []parallel.TaskInit{{Name: "task1", Index: 0}}}
	newModel, _ := m.Update(initMsg)
	m = newModel.(Model)

	syncMsg := TaskSyncingMsg{Name: "task1", Index: 0, Host: "host-a"}
	newModel, _ = m.Update(syncMsg)
	m = newModel.(Model)

	// Requeue
	requeueMsg := TaskRequeuedMsg{
		Name:            "task1",
		Index:           0,
		UnavailableHost: "host-a",
	}
	newModel, _ = m.Update(requeueMsg)
	m = newModel.(Model)

	assert.Equal(t, parallel.TaskPending, m.tasks[0].Status)
	assert.Equal(t, "", m.tasks[0].Host)
	assert.True(t, m.tasks[0].StartTime.IsZero())
	assert.Len(t, m.warnings, 1)
	assert.Contains(t, m.warnings[0], "host-a")
}

func TestModel_AllCompleted(t *testing.T) {
	m := NewModel(2, nil)

	completedMsg := AllCompletedMsg{
		Passed:   1,
		Failed:   1,
		Duration: 5 * time.Second,
	}
	newModel, _ := m.Update(completedMsg)
	m = newModel.(Model)

	assert.True(t, m.completed)
	assert.Equal(t, 1, m.passed)
	assert.Equal(t, 1, m.failed)
	assert.Equal(t, 5*time.Second, m.totalTime)
}

func TestModel_KeyNavigation(t *testing.T) {
	m := NewModel(3, nil)

	// Initialize tasks
	initMsg := InitTasksMsg{
		Tasks: []parallel.TaskInit{
			{Name: "task1", Index: 0},
			{Name: "task2", Index: 1},
			{Name: "task3", Index: 2},
		},
	}
	newModel, _ := m.Update(initMsg)
	m = newModel.(Model)

	// Start at 0
	assert.Equal(t, 0, m.selected)

	// Move down
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = newModel.(Model)
	assert.Equal(t, 1, m.selected)

	// Move down again
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = newModel.(Model)
	assert.Equal(t, 2, m.selected)

	// Can't go past end
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = newModel.(Model)
	assert.Equal(t, 2, m.selected)

	// Move up
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = newModel.(Model)
	assert.Equal(t, 1, m.selected)

	// Jump to top
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = newModel.(Model)
	assert.Equal(t, 0, m.selected)

	// Jump to bottom
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = newModel.(Model)
	assert.Equal(t, 2, m.selected)
}

func TestModel_QuitCancels(t *testing.T) {
	cancelled := false
	cancel := func() { cancelled = true }
	m := NewModel(1, cancel)

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = newModel.(Model)

	assert.True(t, cancelled)
	assert.True(t, m.quitting)
	assert.NotNil(t, cmd) // Should return tea.Quit
}

func TestModel_ViewNotEmpty(t *testing.T) {
	m := NewModel(2, nil)
	m.width = 80
	m.height = 24

	// Initialize tasks
	initMsg := InitTasksMsg{
		Tasks: []parallel.TaskInit{
			{Name: "task1", Index: 0},
			{Name: "task2", Index: 1},
		},
	}
	newModel, _ := m.Update(initMsg)
	m = newModel.(Model)

	view := m.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "task1")
	assert.Contains(t, view, "task2")
	assert.Contains(t, view, "Tasks")
}

func TestModel_ViewShowsQuittingEmpty(t *testing.T) {
	m := NewModel(1, nil)
	m.quitting = true

	view := m.View()
	assert.Empty(t, view)
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{50 * time.Millisecond, "0.05s"},
		{500 * time.Millisecond, "0.5s"},
		{2 * time.Second, "2.0s"},
		{45 * time.Second, "45.0s"},
		{90 * time.Second, "1m30.0s"},
		{125 * time.Second, "2m5.0s"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatDuration(tt.duration)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestModel_WindowSizeMsg(t *testing.T) {
	m := NewModel(1, nil)

	sizeMsg := tea.WindowSizeMsg{Width: 120, Height: 40}
	newModel, _ := m.Update(sizeMsg)
	m = newModel.(Model)

	assert.Equal(t, 120, m.width)
	assert.Equal(t, 40, m.height)
}

func TestModel_TickMsg(t *testing.T) {
	m := NewModel(1, nil)
	initialFrame := m.spinnerFrame

	tickMsg := tickMsg(time.Now())
	newModel, cmd := m.Update(tickMsg)
	m = newModel.(Model)

	assert.NotEqual(t, initialFrame, m.spinnerFrame)
	assert.NotNil(t, cmd) // Should return another tick command
}

func TestModel_OrchestratorDoneMsg(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ctx // unused, just for context.CancelFunc type

	m := NewModel(1, nil)
	m.startTime = time.Now().Add(-2 * time.Second)

	doneMsg := orchestratorDoneMsg{err: nil}
	newModel, _ := m.Update(doneMsg)
	m = newModel.(Model)

	assert.True(t, m.completed)
	assert.True(t, m.totalTime >= 2*time.Second)
}
