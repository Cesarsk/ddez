package ui

import "testing"

func TestResolveTheme(t *testing.T) {
	// "ike" is the signature look: empty and unknown names resolve to it;
	// `theme: default` still restores the original palette.
	if got := ResolveTheme(""); got.Name != "ike" {
		t.Errorf("empty → %q, want ike", got.Name)
	}
	if got := ResolveTheme("MoNo"); got.Name != "mono" { // case-insensitive
		t.Errorf("MoNo → %q, want mono", got.Name)
	}
	if got := ResolveTheme("does-not-exist"); got.Name != "ike" {
		t.Errorf("unknown → %q, want ike fallback", got.Name)
	}
	if got := ResolveTheme("default"); got.Name != "default" {
		t.Errorf("default → %q, want the original palette", got.Name)
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
