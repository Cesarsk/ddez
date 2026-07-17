package ui

import "testing"

func TestResolveTheme(t *testing.T) {
	if got := ResolveTheme(""); got.Name != "default" {
		t.Errorf("empty → %q, want default", got.Name)
	}
	if got := ResolveTheme("MoNo"); got.Name != "mono" { // case-insensitive
		t.Errorf("MoNo → %q, want mono", got.Name)
	}
	if got := ResolveTheme("does-not-exist"); got.Name != "default" {
		t.Errorf("unknown → %q, want default fallback", got.Name)
	}
}

func TestThemeRecolor(t *testing.T) {
	// The default theme's tags are the canonical ones, so recolor is a no-op.
	def := ResolveTheme("default")
	s := "[orange]X[aqua]Y[gray]Z"
	if got := def.recolor(s); got != s {
		t.Errorf("default recolor should be a no-op, got %q", got)
	}
	// A non-default theme remaps the canonical tags to its own.
	mono := ResolveTheme("mono")
	if got := mono.recolor("[orange]A [aqua]B [gray]C"); got != "[white]A [white]B [gray]C" {
		t.Errorf("mono recolor = %q", got)
	}
}
