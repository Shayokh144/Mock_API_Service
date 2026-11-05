package rules

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func sampleDraft() Draft {
	return Draft{
		Name:        "Items",
		Enabled:     true,
		Method:      "get",
		PathPattern: "/v1/items",
		StatusCode:  200,
		Headers:     map[string]string{"x-extra": "yes"},
		Body:        `{"ok": true}`,
	}
}

func TestCreateNormalizesAndMatchesExactPath(t *testing.T) {
	store := NewMemory()
	store.now = func() time.Time {
		return time.Date(2026, 11, 5, 12, 0, 0, 0, time.FixedZone("ICT", 7*3600))
	}
	rule, err := store.Create(sampleDraft())
	if err != nil {
		t.Fatal(err)
	}
	if len(rule.ID) != 16 {
		t.Fatalf("id = %q", rule.ID)
	}
	if rule.Method != "GET" {
		t.Fatalf("method = %s", rule.Method)
	}
	if !rule.CreatedAt.Equal(rule.UpdatedAt.Time) {
		t.Fatal("timestamps differ")
	}
	if rule.CreatedAt.Location() != time.UTC {
		t.Fatalf("location = %s", rule.CreatedAt.Location())
	}
	if string(rule.Body) != `{"ok":true}` {
		t.Fatalf("body = %s", rule.Body)
	}
	if rule.Headers["X-Extra"] != "yes" {
		t.Fatalf("headers = %#v", rule.Headers)
	}

	got, ok := store.Match("GET", "/v1/items")
	if !ok || got.ID != rule.ID {
		t.Fatalf("match = %+v ok=%v", got, ok)
	}
	if _, ok := store.Match("GET", "/v1/items/"); ok {
		t.Fatal("trailing slash matched")
	}
	if _, ok := store.Match("GET", "/v1/Items"); ok {
		t.Fatal("path comparison was case-insensitive")
	}
	if _, ok := store.Match("POST", "/v1/items"); ok {
		t.Fatal("method matched")
	}
	if _, ok := store.Match("get", "/v1/items"); !ok {
		t.Fatal("method comparison was case-sensitive")
	}
}

func TestDisabledRuleDoesNotMatch(t *testing.T) {
	store := NewMemory()
	draft := sampleDraft()
	draft.Enabled = false
	rule, err := store.Create(draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Match("GET", "/v1/items"); ok {
		t.Fatal("disabled rule matched")
	}
	draft.Enabled = true
	if _, err := store.Update(rule.ID, draft); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Match("GET", "/v1/items"); !ok {
		t.Fatal("enabled rule did not match")
	}
}

func TestDuplicateMethodAndPatternIncludesDisabled(t *testing.T) {
	store := NewMemory()
	first := sampleDraft()
	first.Enabled = false
	if _, err := store.Create(first); err != nil {
		t.Fatal(err)
	}
	_, err := store.Create(sampleDraft())
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateKeepsIdentityAndRejectsOtherDuplicate(t *testing.T) {
	store := NewMemory()
	start := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return start }
	first, err := store.Create(sampleDraft())
	if err != nil {
		t.Fatal(err)
	}
	other := sampleDraft()
	other.PathPattern = "/v1/other"
	if _, err := store.Create(other); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return start.Add(time.Minute) }
	updatedDraft := sampleDraft()
	updatedDraft.Name = "Renamed"
	updated, err := store.Update(first.ID, updatedDraft)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != first.ID || !updated.CreatedAt.Equal(first.CreatedAt.Time) {
		t.Fatalf("identity changed: %+v", updated)
	}
	if !updated.UpdatedAt.Equal(start.Add(time.Minute)) {
		t.Fatalf("updated_at = %s", updated.UpdatedAt)
	}
	clash := sampleDraft()
	clash.PathPattern = "/v1/other"
	if _, err := store.Update(first.ID, clash); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeleteAndMissing(t *testing.T) {
	store := NewMemory()
	rule, err := store.Create(sampleDraft())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(rule.ID); !ok {
		t.Fatal("missing created rule")
	}
	if err := store.Delete(rule.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(rule.ID); ok {
		t.Fatal("deleted rule still present")
	}
	if err := store.Delete(rule.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := store.Update(rule.ID, sampleDraft()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update err = %v", err)
	}
}

func TestValidation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Draft)
		substr string
	}{
		{"long name", func(d *Draft) { d.Name = strings.Repeat("a", 81) }, "name"},
		{"bad method", func(d *Draft) { d.Method = "TRACE" }, "method"},
		{"low status", func(d *Draft) { d.StatusCode = 99 }, "status_code"},
		{"high status", func(d *Draft) { d.StatusCode = 600 }, "status_code"},
		{"wildcard", func(d *Draft) { d.PathPattern = "/v1/*" }, "exact"},
		{"admin", func(d *Draft) { d.PathPattern = "/_admin" }, "/_admin"},
		{"admin child", func(d *Draft) { d.PathPattern = "/_admin/rules" }, "/_admin"},
		{"relative", func(d *Draft) { d.PathPattern = "v1/items" }, "start with /"},
		{"empty segment", func(d *Draft) { d.PathPattern = "/v1//items" }, "empty segment"},
		{"bad json", func(d *Draft) { d.Body = `{"ok"` }, "JSON"},
		{"big body", func(d *Draft) { d.Body = `"` + strings.Repeat("a", configMax()+1) + `"` }, "1 MiB"},
		{"bad header", func(d *Draft) { d.Headers = map[string]string{"Bad Name": "x"} }, "header"},
		{"newline header", func(d *Draft) { d.Headers = map[string]string{"X-Ok": "a\nb"} }, "header"},
		{"hop header", func(d *Draft) { d.Headers = map[string]string{"Connection": "close"} }, "hop-by-hop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			draft := sampleDraft()
			tc.change(&draft)
			_, err := NewMemory().Create(draft)
			if err == nil || !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func configMax() int {
	return 1 << 20
}

func TestEmptyBodyIsNullAndRootPathMatches(t *testing.T) {
	store := NewMemory()
	draft := sampleDraft()
	draft.PathPattern = "/"
	draft.Body = "  null  "
	draft.StatusCode = 204
	rule, err := store.Create(draft)
	if err != nil {
		t.Fatal(err)
	}
	if len(rule.Body) != 0 {
		t.Fatalf("body = %s", rule.Body)
	}
	if _, ok := store.Match("GET", "/"); !ok {
		t.Fatal("root did not match")
	}
}

func TestTrailingSlashIsDistinct(t *testing.T) {
	store := NewMemory()
	draft := sampleDraft()
	draft.PathPattern = "/v1/items/"
	if _, err := store.Create(draft); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Match("GET", "/v1/items/"); !ok {
		t.Fatal("trailing slash pattern did not match")
	}
	if _, ok := store.Match("GET", "/v1/items"); ok {
		t.Fatal("trailing slash pattern matched the bare path")
	}
}
