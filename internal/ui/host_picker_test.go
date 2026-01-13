package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHostItem(t *testing.T) {
	host := HostInfo{
		Name: "gpu-box",
		SSH:  []string{"gpu-local", "gpu-vpn"},
		Dir:  "/home/user/project",
		Tags: []string{"gpu", "fast"},
	}

	item := hostItem{host: host}

	t.Run("Title", func(t *testing.T) {
		title := item.Title()
		assert.Contains(t, title, "gpu-box")
	})

	t.Run("Description", func(t *testing.T) {
		desc := item.Description()
		assert.Contains(t, desc, "gpu-local")
		assert.Contains(t, desc, "+1") // Additional SSH alias count
		assert.Contains(t, desc, "/home/user/project")
		assert.Contains(t, desc, "gpu")
		assert.Contains(t, desc, "fast")
	})

	t.Run("FilterValue", func(t *testing.T) {
		filter := item.FilterValue()
		assert.Contains(t, filter, "gpu-box")
		assert.Contains(t, filter, "gpu-local")
		assert.Contains(t, filter, "gpu-vpn")
		assert.Contains(t, filter, "gpu")
		assert.Contains(t, filter, "fast")
	})
}

func TestHostItemNonDefault(t *testing.T) {
	host := HostInfo{
		Name: "simple-host",
		SSH:  []string{"simple"},
	}

	item := hostItem{host: host}

	title := item.Title()
	assert.Equal(t, "simple-host", title)
	assert.NotContains(t, title, "(default)")
}

func TestHostItemSingleSSH(t *testing.T) {
	host := HostInfo{
		Name: "single-ssh",
		SSH:  []string{"myhost"},
	}

	item := hostItem{host: host}
	desc := item.Description()

	assert.Contains(t, desc, "myhost")
	assert.NotContains(t, desc, "+") // No extra count for single alias
}

func TestNewHostPickerModel(t *testing.T) {
	hosts := []HostInfo{
		{Name: "host1", SSH: []string{"h1"}},
		{Name: "host2", SSH: []string{"h2"}},
	}

	model := NewHostPickerModel(hosts)

	assert.Len(t, model.hosts, 2)
	assert.Nil(t, model.selected)
	assert.False(t, model.quitting)
}

func TestHostPickerModelSelected(t *testing.T) {
	hosts := []HostInfo{
		{Name: "host1", SSH: []string{"h1"}},
	}

	model := NewHostPickerModel(hosts)

	// Initially nil
	assert.Nil(t, model.Selected())

	// After setting
	model.selected = &hosts[0]
	selected := model.Selected()
	assert.NotNil(t, selected)
	assert.Equal(t, "host1", selected.Name)
}
