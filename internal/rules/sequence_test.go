package rules

import (
	"testing"
)

func TestMaxUsesStopsAndResets(t *testing.T) {
	store := NewMemory()
	rule, err := store.Create(Draft{
		Name: "Once", Enabled: true, Method: "GET", PathPattern: "/v1/once", StatusCode: 200, MaxUses: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Match("GET", "/v1/once"); !ok {
		t.Fatal("first match failed")
	}
	if _, ok := store.Match("GET", "/v1/once"); ok {
		t.Fatal("second match should be exhausted")
	}
	left, limited := store.UsesLeft(rule.ID)
	if !limited || left != 0 {
		t.Fatalf("left = %d limited %v", left, limited)
	}
	if _, err := store.Update(rule.ID, Draft{
		Name: "Once", Enabled: true, Method: "GET", PathPattern: "/v1/once", StatusCode: 200, MaxUses: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Match("GET", "/v1/once"); !ok {
		t.Fatal("edit did not restore the count")
	}
}

func TestMaxUsesIsPerStore(t *testing.T) {
	draft := Draft{Name: "Once", Enabled: true, Method: "GET", PathPattern: "/v1/once", StatusCode: 200, MaxUses: 1}
	first := NewMemory()
	second := NewMemory()
	if _, err := first.Create(draft); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Create(draft); err != nil {
		t.Fatal(err)
	}
	if _, ok := first.Match("GET", "/v1/once"); !ok {
		t.Fatal("first store")
	}
	if _, ok := second.Match("GET", "/v1/once"); !ok {
		t.Fatal("second store should still have its own count")
	}
}

func TestResponseSequenceSticksThenLoops(t *testing.T) {
	store := NewMemory()
	steps := []ResponseStep{{StatusCode: 200}, {StatusCode: 201}, {StatusCode: 202}}
	if _, err := store.Create(Draft{
		Name: "Seq", Enabled: true, Method: "GET", PathPattern: "/v1/seq", StatusCode: 200,
		Responses: steps, Sequence: "stick",
	}); err != nil {
		t.Fatal(err)
	}
	var got []int
	for i := 0; i < 4; i++ {
		rule, ok := store.Match("GET", "/v1/seq")
		if !ok {
			t.Fatal("missing")
		}
		got = append(got, rule.StatusCode)
		if rule.AppliedStep != i+1 && i < 3 {
			if i >= 2 && rule.AppliedStep != 3 {
				t.Fatalf("step %d = %d", i, rule.AppliedStep)
			}
		}
	}
	if got[0] != 200 || got[1] != 201 || got[2] != 202 || got[3] != 202 {
		t.Fatalf("stick = %v", got)
	}
	store.ResetRuntime()
	if _, err := store.Update(store.List()[0].ID, Draft{
		Name: "Seq", Enabled: true, Method: "GET", PathPattern: "/v1/seq", StatusCode: 200,
		Responses: steps, Sequence: "loop",
	}); err != nil {
		t.Fatal(err)
	}
	got = nil
	for i := 0; i < 4; i++ {
		rule, ok := store.Match("GET", "/v1/seq")
		if !ok {
			t.Fatal("missing")
		}
		got = append(got, rule.StatusCode)
	}
	if got[0] != 200 || got[1] != 201 || got[2] != 202 || got[3] != 200 {
		t.Fatalf("loop = %v", got)
	}
}
