package p

import (
	"strings"
	"testing"
)

func TestFizzBuzz(t *testing.T) {
	got := strings.Join(FizzBuzz(15), ",")
	if want := "1,2,Fizz,4,Buzz,Fizz,7,8,Fizz,Buzz,11,Fizz,13,14,FizzBuzz"; got != want {
		t.Fatalf("FizzBuzz(15) = %s", got)
	}
}
