//go:build harness

package api

import "testing"

func TestClientBoundsRequestsButNotTheStream(t *testing.T) {
	c := &Client{}
	if c.HTTPClient().Timeout != DefaultTimeout {
		t.Fatalf("request timeout = %v", c.HTTPClient().Timeout)
	}
	if c.streamClient().Timeout != 0 {
		t.Fatal("the event stream would be cut by the request timeout")
	}
}
