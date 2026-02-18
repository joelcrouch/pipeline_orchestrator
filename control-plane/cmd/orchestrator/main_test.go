package main

import "testing"

func TestEnvOr(t *testing.T) {
	t.Setenv("TEST_KEY", "hello")

	got := envOr("TEST_KEY", "fallback")
	if got != "hello" {
		t.Errorf("Expected 'hello', got '&s', got")
	}

	got = envOr("MISSING_KEY", "fallback")
	if got != "fallback" {
		t.Errorf("expected 'fallback', got '%s'", got)
	}
}
