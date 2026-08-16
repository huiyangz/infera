package testrunner

import (
	"context"
	"strings"
	"testing"
)

func TestLocalPass(t *testing.T) {
	l := &Local{Script: "true"}
	pass, out, err := l.RunTests(context.Background(), ".")
	if err != nil || !pass {
		t.Fatalf("want pass=true err=nil, got pass=%v err=%v out=%q", pass, err, out)
	}
}

func TestLocalFail(t *testing.T) {
	l := &Local{Script: "echo boom; false"}
	pass, out, err := l.RunTests(context.Background(), ".")
	if err != nil || pass {
		t.Fatalf("want pass=false err=nil, got pass=%v err=%v", pass, err)
	}
	if !strings.Contains(out, "boom") {
		t.Fatalf("want output to contain 'boom', got %q", out)
	}
}
