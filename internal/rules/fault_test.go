package rules

import (
	"strings"
	"testing"
)

func TestFaultValidation(t *testing.T) {
	store := NewMemory()
	saved, err := store.Create(Draft{
		Name: "Drop", Enabled: true, Method: "GET", PathPattern: "/v1/drop", StatusCode: 200, Fault: "RESET",
	})
	if err != nil || saved.Fault != "reset" {
		t.Fatalf("saved = %+v %v", saved, err)
	}
	if _, err := store.Create(Draft{
		Enabled: true, Method: "GET", PathPattern: "/v1/bad", StatusCode: 200, Fault: "drop",
	}); err == nil || !strings.Contains(err.Error(), "fault") {
		t.Fatalf("bad fault = %v", err)
	}
	if _, err := store.Create(Draft{
		Enabled: true, FreshRequest: true, Method: "GET", PathPattern: "/v1/fresh", StatusCode: 200, Fault: "hang",
	}); err == nil || !strings.Contains(err.Error(), "fresh") {
		t.Fatalf("fresh fault = %v", err)
	}
}
