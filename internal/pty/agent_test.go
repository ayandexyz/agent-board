package pty

import (
	"reflect"
	"testing"
)

func TestFreeCodeReadyPatternMatchesBanner(t *testing.T) {
	cfg := NewFreeCode()
	readyRe := cfg.ReadyPattern

	banner := StripANSI("                       >_ FreeCode (v0.24.6)                      ")
	if !readyRe.MatchString(banner) {
		t.Fatal("FreeCode banner subtitle should mark the agent as ready")
	}

	if readyRe.MatchString("I loaded the FreeCode config from disk") {
		t.Fatal("Ready pattern is too loose — matched free-form prose")
	}
}

func TestFreeCodeIsInteractiveTUI(t *testing.T) {
	cfg := NewFreeCode()

	for _, a := range cfg.Args {
		if a == "run" {
			t.Fatal("FreeCode must open its TUI, not the headless 'run' subcommand")
		}
	}

	if cfg.SendPrompt == nil {
		t.Fatal("FreeCode needs SendPrompt set to type the prompt into the TUI editor")
	}

	if reflect.ValueOf(cfg.SendPrompt).Pointer() != reflect.ValueOf(SendPromptSingleLine).Pointer() {
		t.Fatalf("expected SendPromptSingleLine, got %T", cfg.SendPrompt)
	}

	if cfg.Bin != "freecode" {
		t.Fatalf("expected Bin=freecode, got %q", cfg.Bin)
	}
}

func TestFreeCodeIdlePatternMatchesIdlePrompt(t *testing.T) {
	cfg := NewFreeCode()
	if len(cfg.IdlePatterns) == 0 {
		t.Fatal("FreeCode needs IdlePatterns to detect completion")
	}

	emptyPrompt := StripANSI("❯ ")
	matched := false
	for _, re := range cfg.IdlePatterns {
		if re.MatchString(emptyPrompt) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("no idle pattern matched the empty TUI prompt %q", emptyPrompt)
	}
}

func TestFreeCodeIsRegistered(t *testing.T) {
	cfg := GetConfig("freecode")
	if cfg == nil {
		t.Fatal("FreeCode config not registered under key \"freecode\"")
	}
	if cfg.Bin != "freecode" {
		t.Fatalf("registered freecode config has Bin=%q, want \"freecode\"", cfg.Bin)
	}
}

func TestAntigravityReadyPatternMatchesBanner(t *testing.T) {
	cfg := NewAntigravity()
	readyRe := cfg.ReadyPattern

	footer := StripANSI("? for shortcuts")
	if !readyRe.MatchString(footer) {
		t.Fatal("Antigravity footer hint should mark the agent as ready")
	}

	// The agent is launched inside a tmux window running an interactive
	// shell (e.g. starship), whose own prompt commonly ends in "❯". If the
	// ready pattern matches that bare caret, injectPrompt fires before `agy`
	// is even started and the prompt gets typed into the shell instead.
	shellPrompt := StripANSI("agent-board main ❯ ")
	if readyRe.MatchString(shellPrompt) {
		t.Fatal("a bare wrapping-shell prompt must not mark Antigravity as ready")
	}

	// "Antigravity CLI" also appears on the pre-auth splash screen ("Welcome
	// to the Antigravity CLI. You are currently not signed in. Signing
	// in...") before the input box exists. Matching on that text alone fires
	// injection too early and the prompt is typed into a screen with no
	// listener yet, so it's silently lost.
	signingIn := StripANSI("Welcome to the Antigravity CLI. You are currently not signed in.\n\n ⣾  Signing in...")
	if readyRe.MatchString(signingIn) {
		t.Fatal("the pre-auth signing-in screen must not mark Antigravity as ready")
	}
}

func TestAntigravityIsInteractive(t *testing.T) {
	cfg := NewAntigravity()

	if cfg.SendPrompt == nil {
		t.Fatal("Antigravity needs SendPrompt set")
	}

	if reflect.ValueOf(cfg.SendPrompt).Pointer() != reflect.ValueOf(SendPromptSingleLine).Pointer() {
		t.Fatalf("expected SendPromptSingleLine, got %T", cfg.SendPrompt)
	}

	if cfg.Bin != "agy" {
		t.Fatalf("expected Bin=agy, got %q", cfg.Bin)
	}
}

func TestAntigravityIdlePatternMatchesIdlePrompt(t *testing.T) {
	cfg := NewAntigravity()
	if len(cfg.IdlePatterns) == 0 {
		t.Fatal("Antigravity needs IdlePatterns to detect completion")
	}

	footer := StripANSI("? for shortcuts")
	matched := false
	for _, re := range cfg.IdlePatterns {
		if re.MatchString(footer) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("no idle pattern matched the footer %q", footer)
	}
}

func TestAntigravityIsRegistered(t *testing.T) {
	for _, key := range []string{"antigravity", "agy"} {
		cfg := GetConfig(key)
		if cfg == nil {
			t.Fatalf("Antigravity config not registered under key %q", key)
		}
		if cfg.Bin != "agy" {
			t.Fatalf("registered config under %q has Bin=%q, want \"agy\"", key, cfg.Bin)
		}
	}
}