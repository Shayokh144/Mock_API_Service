package history

import (
	"net/http"
	"testing"
	"time"
)

func TestAddListCapDeleteAndClear(t *testing.T) {
	log := New(2)
	start := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	step := 0
	log.now = func() time.Time {
		step++
		return start.Add(time.Duration(step) * time.Second)
	}
	first := log.Add(Entry{Method: "GET", Path: "/one", Source: SourceUpstream, Status: 200})
	second := log.Add(Entry{Method: "POST", Path: "/two", Source: SourceMock, Status: 201, RuleID: "abc"})
	third := log.Add(Entry{Method: "PUT", Path: "/three", Source: SourceUpstreamError, Status: 502})

	list := log.List()
	if len(list) != 2 {
		t.Fatalf("len = %d", len(list))
	}
	if list[0].ID != third.ID || list[1].ID != second.ID {
		t.Fatalf("order = %#v", list)
	}
	if _, ok := log.Get(first.ID); ok {
		t.Fatal("oldest entry was kept")
	}
	got, ok := log.Get(third.ID)
	if !ok || got.Path != "/three" || !got.StartedAt.Equal(start.Add(3*time.Second)) {
		t.Fatalf("got = %+v ok=%v", got, ok)
	}

	if !log.Delete(second.ID) {
		t.Fatal("delete missed")
	}
	if log.Delete(second.ID) {
		t.Fatal("second delete succeeded")
	}
	if len(log.List()) != 1 {
		t.Fatalf("after delete = %d", len(log.List()))
	}
	log.Clear()
	if len(log.List()) != 0 {
		t.Fatal("clear left entries")
	}
}

func TestNewLogIsEmpty(t *testing.T) {
	if len(New(5).List()) != 0 {
		t.Fatal("new log is not empty")
	}
}

func TestHeaderCopiesDoNotAlias(t *testing.T) {
	log := New(5)
	header := http.Header{"X-Token": []string{"secret"}}
	entry := log.Add(Entry{RequestHeaders: header, ResponseHeaders: header})
	header.Set("X-Token", "changed")
	got, _ := log.Get(entry.ID)
	if got.RequestHeaders.Get("X-Token") != "secret" || got.ResponseHeaders.Get("X-Token") != "secret" {
		t.Fatalf("headers aliased: %+v %+v", got.RequestHeaders, got.ResponseHeaders)
	}
}
