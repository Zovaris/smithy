package p

import (
	"os"
	"strings"
	"testing"
)

func TestGreetingMoved(t *testing.T) {
	if Greeting("Ana") != "Hello, Ana!" || Shout("hi") != "HI" {
		t.Fatal("behaviour changed")
	}
	g, err := os.ReadFile("greet.go")
	if err != nil || !strings.Contains(string(g), "func Greeting(") {
		t.Fatal("greet.go does not define Greeting")
	}
	u, _ := os.ReadFile("util.go")
	if strings.Contains(string(u), "func Greeting(") {
		t.Fatal("util.go still defines Greeting")
	}
}
