package acp

import (
	"io"
	"strings"
	"testing"
)

func TestModelsFromConfigOptions(t *testing.T) {
	from := strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}
{"jsonrpc":"2.0","id":2,"result":{"sessionId":"s1","configOptions":[{"id":"mode","category":"mode","options":[{"value":"build"}]},{"id":"model","category":"model","currentValue":"oc/b","options":[{"value":"oc/a","name":"A"},{"value":"oc/b","name":"B"}]}]}}
`)
	got, err := Models(from, io.Discard, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if got.Current != "oc/b" || len(got.Models) != 2 || got.Models[0].ID != "oc/a" || got.Models[0].Name != "A" {
		t.Fatalf("got %+v", got)
	}
}

func TestModelsFromAvailableModels(t *testing.T) {
	from := strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}
{"jsonrpc":"2.0","id":2,"result":{"sessionId":"s1","models":{"currentModelId":"g-pro","availableModels":[{"modelId":"g-pro","name":"Pro"},{"modelId":"g-flash","name":"Flash"}]}}}
`)
	got, err := Models(from, io.Discard, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if got.Current != "g-pro" || len(got.Models) != 2 || got.Models[1].ID != "g-flash" {
		t.Fatalf("got %+v", got)
	}
}

func TestModelsNoneReported(t *testing.T) {
	from := strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}
{"jsonrpc":"2.0","id":2,"result":{"sessionId":"s1"}}
`)
	got, err := Models(from, io.Discard, "/tmp")
	if err != nil || len(got.Models) != 0 {
		t.Fatalf("got %+v %v", got, err)
	}
}
