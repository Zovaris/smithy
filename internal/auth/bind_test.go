package auth

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func TestBindKeepsKeyAndSetKeepsBinding(t *testing.T) {
	home(t)
	if err := Bind("linear", "api.linear.app", nil, "", ""); err == nil {
		t.Fatal("bind with no key should fail")
	}
	if err := Set("linear", "lin_api_secretabcd"); err != nil {
		t.Fatal(err)
	}
	if err := Bind("linear", "api.linear.app", nil, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := Bind("linear", "API.linear.app", []string{"get", "post"}, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := SetAsk("linear", true); err != nil {
		t.Fatal(err)
	}
	if err := Set("linear", "lin_api_secretwxyz"); err != nil {
		t.Fatal(err)
	}
	s, ok := Secret("linear")
	if !ok || s.Value != "lin_api_secretwxyz" || !slices.Equal(s.Hosts, []string{"api.linear.app"}) ||
		!slices.Equal(s.Methods, []string{"GET", "POST"}) || !s.Ask || s.Header != "Authorization" {
		t.Fatalf("Secret = %+v %v", s, ok)
	}
	l, _ := List()
	if len(l) != 1 || !strings.Contains(l[0], "api.linear.app") || strings.Contains(l[0], "secretwxyz") {
		t.Errorf("List = %q", l)
	}
}

func TestBindRejectsBadHostsAndMethods(t *testing.T) {
	home(t)
	_ = Set("linear", "lin_api_secretabcd")
	for _, h := range []string{"https://api.linear.app", "api.linear.app:443", "api.linear.app/x", "", "*", "1.2.3.4"} {
		if Bind("linear", h, nil, "", "") == nil {
			t.Errorf("host %q accepted", h)
		}
	}
	if Bind("linear", "api.linear.app", []string{"FETCH"}, "", "") == nil {
		t.Error("bad method accepted")
	}
	if Bind("linear", "api.linear.app", nil, "Bad Header", "") == nil {
		t.Error("bad header accepted")
	}
	if _, ok := Secret("linear"); ok {
		t.Error("unbound key is a proxy secret")
	}
}

func TestMainBindAndAsk(t *testing.T) {
	home(t)
	_ = Set("gh", "ghp_secretsecretabcd")
	var out, errb bytes.Buffer
	if rc := Main([]string{"bind", "gh", "api.github.com", "GET,POST", "--prefix", "Bearer"}, nil, &out, &errb); rc != 0 {
		t.Fatalf("bind rc %d %s", rc, errb.String())
	}
	if rc := Main([]string{"ask", "gh", "on"}, nil, &out, &errb); rc != 0 {
		t.Fatalf("ask rc %d %s", rc, errb.String())
	}
	s, _ := Secret("gh")
	if s.Prefix != "Bearer" || !s.Ask || !slices.Equal(s.Methods, []string{"GET", "POST"}) {
		t.Errorf("secret %+v", s)
	}
	if strings.Contains(out.String(), "secretsecret") {
		t.Errorf("key printed: %s", out.String())
	}
}
