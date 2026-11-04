package main

import "testing"

func TestSelfOperationCompletionMessage(t *testing.T) {
	cases := map[string]string{
		"restart":      "Self-restart completed successfully",
		"label_change": "Label change completed successfully after restart",
		"update":       "Self-update completed successfully after restart",
	}

	for opType, want := range cases {
		if got := selfOperationCompletionMessage(opType); got != want {
			t.Errorf("selfOperationCompletionMessage(%q) = %q, want %q", opType, got, want)
		}
	}
}
