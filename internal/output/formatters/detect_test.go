package formatters

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHasIntentionalZeroFlag guards against substring matching. "--co" is a
// prefix of "--cov", "--color", "--config", and
// "--continue-on-collection-errors", so a strings.Contains check silently
// disabled no-tests detection for `pytest --cov=app -k typo` and most other
// real CI commands - the feature was off exactly where it mattered most.
func TestHasIntentionalZeroFlag(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"plain pytest", "pytest -k typo", false},
		{"collect-only", "pytest --collect-only tests/", true},
		{"co abbreviation", "pytest --co tests/", true},
		{"fixtures", "pytest --fixtures", true},
		{"markers", "pytest --markers", true},
		{"passWithNoTests", "jest --passWithNoTests", true},
		{"listTests", "jest --listTests", true},

		// Flags that merely start with an opt-out flag's name.
		{"coverage flag", "pytest --cov=app -k typo", false},
		{"cov-report", "pytest --cov-report=xml tests/", false},
		{"color flag", "pytest --color=yes -k typo", false},
		{"config flag", "pytest --config=setup.cfg -k typo", false},
		{"count flag", "pytest --count=3 tests/", false},
		{"continue-on-collection-errors", "pytest --continue-on-collection-errors", false},
		{"vitest coverage", "vitest run --coverage", false},

		{"flag with = value still matches", "pytest --collect-only=x tests/", true},
		{"case insensitive", "jest --PASSWITHNOTESTS", true},
		{"flag name inside a path is not a flag", "pytest tests/--collect-only-fixture", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasIntentionalZeroFlag(tt.command))
		})
	}
}
