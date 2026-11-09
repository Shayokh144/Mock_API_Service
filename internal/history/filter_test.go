package history

import "testing"

func TestParseSource(t *testing.T) {
	cases := []struct {
		raw  string
		want Source
		ok   bool
	}{
		{raw: "", ok: false},
		{raw: "   ", ok: false},
		{raw: "nope", ok: false},
		{raw: "Mock", ok: false},
		{raw: "mock", want: SourceMock, ok: true},
		{raw: " upstream ", want: SourceUpstream, ok: true},
		{raw: "upstream-error", want: SourceUpstreamError, ok: true},
		{raw: "rejected", want: SourceRejected, ok: true},
	}
	for _, tc := range cases {
		got, ok := ParseSource(tc.raw)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("ParseSource(%q) = %q, %v", tc.raw, got, ok)
		}
	}
}

func TestFilterKeepsMatchingSourceAndOrder(t *testing.T) {
	entries := []Entry{
		{ID: "1", Source: SourceMock},
		{ID: "2", Source: SourceUpstream, RuleID: "rule"},
		{ID: "3", Source: SourceUpstream},
		{ID: "4", Source: SourceUpstreamError, RuleID: "rule"},
		{ID: "5", Source: SourceRejected},
	}
	got := Filter(entries, SourceUpstream)
	if len(got) != 2 || got[0].ID != "2" || got[1].ID != "3" {
		t.Fatalf("upstream filter = %+v", got)
	}
	if got[0].RuleID != "rule" || got[1].RuleID != "" {
		t.Fatal("fresh-request upstream entry was dropped or rewritten")
	}
	mocks := Filter(entries, SourceMock)
	if len(mocks) != 1 || mocks[0].ID != "1" {
		t.Fatalf("mock filter = %+v", mocks)
	}
	if len(Filter(nil, SourceMock)) != 0 {
		t.Fatal("nil input was not empty")
	}
}

func TestParseAndFilterUpstream(t *testing.T) {
	if id, ok := ParseUpstream("  any "); !ok || id != UpstreamAny {
		t.Fatalf("any = %q %v", id, ok)
	}
	if _, ok := ParseUpstream("nope"); ok {
		t.Fatal("unknown value was accepted")
	}
	if _, ok := ParseUpstream(""); ok {
		t.Fatal("empty value was accepted")
	}
	id, ok := ParseUpstream("AABBCCDDEEFF0011")
	if !ok || id != "aabbccddeeff0011" {
		t.Fatalf("hex = %q %v", id, ok)
	}
	entries := []Entry{
		{ID: "1", UpstreamID: UpstreamAny, Source: SourceMock},
		{ID: "2", UpstreamID: "aabbccddeeff0011", Source: SourceUpstream},
		{ID: "3", UpstreamID: "aabbccddeeff0011", Source: SourceMock},
	}
	got := FilterUpstream(entries, "aabbccddeeff0011")
	if len(got) != 2 || got[0].ID != "2" || got[1].ID != "3" {
		t.Fatalf("id filter = %+v", got)
	}
	anyRows := FilterUpstream(entries, UpstreamAny)
	if len(anyRows) != 1 || anyRows[0].ID != "1" {
		t.Fatalf("any filter = %+v", anyRows)
	}
}
