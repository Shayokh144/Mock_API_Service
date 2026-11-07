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
