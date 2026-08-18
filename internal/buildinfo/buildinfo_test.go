package buildinfo

import (
	"strings"
	"testing"
)

func TestStringIncludesProgramAndVersion(t *testing.T) {
	got := String("powercheck-test")
	if !strings.Contains(got, "powercheck-test") || !strings.Contains(got, Version) {
		t.Fatalf("unexpected build information: %q", got)
	}
}
