package proof

import "testing"

func TestShownLeavesOneCommandLineBare(t *testing.T) {
	for want, argv := range map[string][]string{
		"npm test":         {"npm test"},
		"go test './x y'":  {"go", "test", "./x y"},
		"npx vitest run a": {"npx", "vitest", "run", "a"},
	} {
		if got := Shown(argv); got != want {
			t.Errorf("Shown(%q) = %q, want %q", argv, got, want)
		}
	}
}
