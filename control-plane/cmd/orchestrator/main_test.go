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
	t.Setenv("FLAG", "true")
	if !boolEnv("FLAG") {
		t.Error("expected true for FLAG=true")
	}

	t.Setenv("FLAG", "false")
	if boolEnv("FLAG") {
		t.Error("expected false for FLAG=false")
	}

	t.Setenv("FLAG", "1")
	if boolEnv("FLAG") {
		t.Error("expected false for FLAG=1 (only 'true' is true)")
	}
}

func TestSplitCSV(t *testing.T) {
	if got := splitCSV(""); got != nil {
		t.Errorf("expected nil for empty string, got %v", got)
	}

	got := splitCSV("a,b,c")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("unexpected result: %v", got)
	}

	got = splitCSV("single")
	if len(got) != 1 || got[0] != "single" {
		t.Errorf("unexpected result: %v", got)
	}
}
