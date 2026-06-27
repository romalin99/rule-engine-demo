package model_test

import (
	"testing"

	"tcg-rulex-engine/internal/model"
)

// TestPlayerBeatTimelimitUsage_IntervalSeconds covers the interval-unit
// normalisation: every accepted unit spelling, case-insensitivity, surrounding
// whitespace, the zero-interval edge case, and the unrecognised-unit path that
// the consumer relies on to skip a malformed beat.
func TestPlayerBeatTimelimitUsage_IntervalSeconds(t *testing.T) {
	tests := []struct {
		name     string
		unit     string
		interval int64
		wantSec  int64
		wantOK   bool
	}{
		{"second canonical", "SECOND", 30, 30, true},
		{"second plural", "SECONDS", 30, 30, true},
		{"second abbrev sec", "sec", 45, 45, true},
		{"second abbrev s", "s", 5, 5, true},
		{"minute", "MINUTE", 2, 120, true},
		{"minute plural", "MINUTES", 2, 120, true},
		{"minute abbrev", "min", 3, 180, true},
		{"hour", "HOUR", 1, 3600, true},
		{"hour abbrev h", "h", 2, 7200, true},
		{"day", "DAY", 1, 86400, true},
		{"day abbrev d", "d", 2, 172800, true},
		{"lowercase unit", "minute", 1, 60, true},
		{"mixed case unit", "Hour", 1, 3600, true},
		{"surrounding whitespace", "  SECOND  ", 10, 10, true},
		{"zero interval, known unit", "SECOND", 0, 0, true},
		{"unknown unit", "FORTNIGHT", 1, 0, false},
		{"empty unit", "", 5, 0, false},
		{"whitespace-only unit", "   ", 5, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model.PlayerBeatTimelimitUsage{IntervalUnit: tt.unit, Interval: tt.interval}
			gotSec, gotOK := m.IntervalSeconds()
			if gotSec != tt.wantSec || gotOK != tt.wantOK {
				t.Errorf("IntervalSeconds(unit=%q, interval=%d) = (%d, %t); want (%d, %t)",
					tt.unit, tt.interval, gotSec, gotOK, tt.wantSec, tt.wantOK)
			}
		})
	}
}
