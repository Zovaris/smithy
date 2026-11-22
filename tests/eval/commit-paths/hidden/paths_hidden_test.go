package commitpaths

import "testing"

func TestCheck(t *testing.T) {
	on := func(p string) bool { return p == "a.txt" }
	tr := func(p string) bool { return p == "gone.txt" }
	if err := Check([]string{"a.txt", "gone.txt"}, on, tr); err != nil {
		t.Errorf("valid paths rejected: %v", err)
	}
	err := Check([]string{"a.txt", "nosuch.txt", "other.txt"}, on, tr)
	if err == nil || err.Error() != "no such path: nosuch.txt" {
		t.Errorf("got %v", err)
	}
	if err := Check(nil, on, tr); err != nil {
		t.Errorf("empty list: %v", err)
	}
}
