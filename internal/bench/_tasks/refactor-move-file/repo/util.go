package p

import "strings"

// Shout upper-cases s.
func Shout(s string) string { return strings.ToUpper(s) }

// Greeting greets name.
func Greeting(name string) string { return "Hello, " + name + "!" }
