package main

import (
	"testing"
)

func TestEnvOr(t *testing.T) {
	t.Setenv("TEST_KEY", "hello")

	got := envOr("TEST_KEY", "fallback")
	if got != "hello" {
		t.Errorf("expected 'hello', got '%s'", got)
	}

	got = envOr("MISSING_KEY", "fallback")
	if got != "fallback" {
		t.Errorf("expected 'fallback', got '%s'", got)
	}
}

func TestBoolEnv(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"true", true},
		{"false", false},
		{"1", false},    // only the exact string "true" counts
		{"TRUE", false}, // case-sensitive
		{"", false},
	}

	for _, tc := range cases {
		t.Setenv("TEST_BOOL", tc.val)
		got := boolEnv("TEST_BOOL")
		if got != tc.want {
			t.Errorf("boolEnv with %q: expected %v, got %v", tc.val, tc.want, got)
		}
	}

	// Unset variable should return false
	t.Setenv("TEST_BOOL", "")
	if boolEnv("UNSET_BOOL_KEY") {
		t.Error("expected false for unset env var, got true")
	}
}

// package main

// import "testing"

// func TestEnvOr(t *testing.T) {
// 	t.Setenv("TEST_KEY", "hello")

// 	got := envOr("TEST_KEY", "fallback")
// 	if got != "hello" {
// 		t.Errorf("Expected 'hello', got '&s', got")
// 	}

// 	got = envOr("MISSING_KEY", "fallback")
// 	if got != "fallback" {
// 		t.Errorf("expected 'fallback', got '%s'", got)
// 	}
// }
