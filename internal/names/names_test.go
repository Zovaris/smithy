package names

import "testing"

func TestBranch(t *testing.T) {
	t.Setenv("SMITHY_CONFIG", t.TempDir()+"/config.json")
	cases := []struct{ key, title, want string }{
		{"PROJ-123", "Add login with Google", "feat/PROJ-123-add-login-google"},
		{"#42", "Fix null crash", "fix/42-null-crash"},
		{"-", "Update README", "docs/update-readme"},
		{"03", "Añadir búsqueda", "feat/03-anadir-busqueda"},
		{"lighter-warden", "Lighter warden and smelt from A/B", "feat/lighter-warden-smelt"},
		{"-", "Make the batch check run once at the end of a warden batch", "feat/make-batch-check-run"},
	}
	for _, c := range cases {
		if got := Branch(c.key, c.title, ""); got != c.want {
			t.Errorf("Branch(%q, %q) = %q, want %q", c.key, c.title, got, c.want)
		}
	}
}

func TestSlugCap(t *testing.T) {
	if s := Slug("a very long title that keeps going and going past forty chars"); len(s) > 40 || s[len(s)-1] == '-' {
		t.Errorf("Slug too long or ends in a dash: %q", s)
	}
}

func TestTypeDocsAndFeatures(t *testing.T) {
	cases := map[string]string{"split json output": "feat", "CONTRIBUTING doc": "docs", "trial report": "docs", "rename flag": "refactor"}
	for in, want := range cases {
		if got := Type(in); got != want {
			t.Errorf("%q → %s, want %s", in, got, want)
		}
	}
}

func TestTypeAddressIsNotDocs(t *testing.T) {
	if got := Type("validate email address"); got != "feat" {
		t.Fatalf("got %s", got)
	}
}
