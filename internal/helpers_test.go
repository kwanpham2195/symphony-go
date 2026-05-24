package internal

import "testing"

func TestIntFromAny(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want int
	}{
		{"float64", float64(42), 42},
		{"int", 7, 7},
		{"int64", int64(99), 99},
		{"string", "nope", 0},
		{"nil", nil, 0},
		{"bool", true, 0},
		{"negative float64", float64(-3), -3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IntFromAny(tt.in)
			if got != tt.want {
				t.Errorf("IntFromAny(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
