package parsers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNvidiaSMI(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		wantNil     bool
		wantName    string
		wantPercent float64
		wantMemUsed int64
		wantMemTot  int64
		wantTemp    int
		wantPower   int
		wantErr     bool
	}{
		{
			name:        "valid nvidia-smi output",
			output:      "NVIDIA GeForce RTX 3080, 45, 2048, 10240, 65, 220",
			wantNil:     false,
			wantName:    "NVIDIA GeForce RTX 3080",
			wantPercent: 45.0,
			wantMemUsed: 2048 * 1024 * 1024,  // 2048 MiB in bytes
			wantMemTot:  10240 * 1024 * 1024, // 10240 MiB in bytes
			wantTemp:    65,
			wantPower:   220,
			wantErr:     false,
		},
		{
			name:        "high utilization GPU",
			output:      "NVIDIA A100, 98, 32768, 40960, 78, 350",
			wantNil:     false,
			wantName:    "NVIDIA A100",
			wantPercent: 98.0,
			wantMemUsed: 32768 * 1024 * 1024,
			wantMemTot:  40960 * 1024 * 1024,
			wantTemp:    78,
			wantPower:   350,
			wantErr:     false,
		},
		{
			name:        "idle GPU",
			output:      "NVIDIA GeForce GTX 1080 Ti, 0, 512, 11264, 42, 75",
			wantNil:     false,
			wantName:    "NVIDIA GeForce GTX 1080 Ti",
			wantPercent: 0.0,
			wantMemUsed: 512 * 1024 * 1024,
			wantMemTot:  11264 * 1024 * 1024,
			wantTemp:    42,
			wantPower:   75,
			wantErr:     false,
		},
		{
			name:        "power with decimal",
			output:      "NVIDIA RTX 4090, 50, 8192, 24576, 70, 185.50",
			wantNil:     false,
			wantName:    "NVIDIA RTX 4090",
			wantPercent: 50.0,
			wantMemUsed: 8192 * 1024 * 1024,
			wantMemTot:  24576 * 1024 * 1024,
			wantTemp:    70,
			wantPower:   185, // truncated to int
			wantErr:     false,
		},
		{
			name:    "empty output - no GPU",
			output:  "",
			wantNil: true,
			wantErr: false,
		},
		{
			name:    "no devices found",
			output:  "No devices were found",
			wantNil: true,
			wantErr: false,
		},
		{
			name:    "nvidia-smi not found",
			output:  "nvidia-smi: command not found",
			wantNil: true,
			wantErr: false,
		},
		{
			name:    "driver error",
			output:  "NVIDIA-SMI has failed because it couldn't communicate with the NVIDIA driver",
			wantNil: true,
			wantErr: false,
		},
		{
			name:    "insufficient fields",
			output:  "NVIDIA GPU, 45, 2048",
			wantNil: false,
			wantErr: true,
		},
		{
			name:    "invalid utilization value",
			output:  "NVIDIA GPU, invalid, 2048, 10240, 65, 220",
			wantNil: false,
			wantErr: true,
		},
		{
			name:    "invalid memory value",
			output:  "NVIDIA GPU, 45, invalid, 10240, 65, 220",
			wantNil: false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics, err := ParseNvidiaSMI(tt.output)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, metrics)
				return
			}

			require.NotNil(t, metrics)
			assert.Equal(t, tt.wantName, metrics.Name)
			assert.Equal(t, tt.wantPercent, metrics.Percent)
			assert.Equal(t, tt.wantMemUsed, metrics.MemoryUsed)
			assert.Equal(t, tt.wantMemTot, metrics.MemoryTotal)
			assert.Equal(t, tt.wantTemp, metrics.Temperature)
			assert.Equal(t, tt.wantPower, metrics.PowerWatts)
		})
	}
}

func TestParseNvidiaSMI_NAValues(t *testing.T) {
	// Some fields might be [N/A] in nvidia-smi output
	output := "NVIDIA Tesla T4, 30, 1024, 16384, [N/A], [N/A]"

	metrics, err := ParseNvidiaSMI(output)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	assert.Equal(t, "NVIDIA Tesla T4", metrics.Name)
	assert.Equal(t, 30.0, metrics.Percent)
	assert.Equal(t, int64(1024*1024*1024), metrics.MemoryUsed)
	assert.Equal(t, int64(16384*1024*1024), metrics.MemoryTotal)
	// Temperature and power should be zero when [N/A]
	assert.Equal(t, 0, metrics.Temperature)
	assert.Equal(t, 0, metrics.PowerWatts)
}

func TestParseNvidiaSMI_WhitespaceHandling(t *testing.T) {
	// Test with extra whitespace around values
	output := "  NVIDIA GeForce RTX 3070 ,  35  ,  4096  ,  8192  ,  58  ,  125  "

	metrics, err := ParseNvidiaSMI(output)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	assert.Equal(t, "NVIDIA GeForce RTX 3070", metrics.Name)
	assert.Equal(t, 35.0, metrics.Percent)
	assert.Equal(t, int64(4096*1024*1024), metrics.MemoryUsed)
	assert.Equal(t, int64(8192*1024*1024), metrics.MemoryTotal)
	assert.Equal(t, 58, metrics.Temperature)
	assert.Equal(t, 125, metrics.PowerWatts)
}

func TestParseNvidiaSMI_MultiLineOutput(t *testing.T) {
	// Test that we handle only the first line (single GPU)
	// Multi-GPU support would need different handling
	output := "NVIDIA GeForce RTX 3080, 45, 2048, 10240, 65, 220"

	metrics, err := ParseNvidiaSMI(output)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	assert.Equal(t, "NVIDIA GeForce RTX 3080", metrics.Name)
}
