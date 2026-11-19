package rules

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDelayBoundsAndOldFile(t *testing.T) {
	store := NewMemory()
	draft := sampleDraft()
	draft.DelayMS = 1500
	saved, err := store.Create(draft)
	if err != nil || saved.DelayMS != 1500 {
		t.Fatalf("saved = %+v err=%v", saved, err)
	}
	draft.DelayMS = 120000
	draft.PathPattern = "/v1/long"
	long, err := store.Create(draft)
	if err != nil || long.DelayMS != 120000 {
		t.Fatalf("long = %+v err=%v", long, err)
	}
	draft.DelayMS = -1
	draft.PathPattern = "/v1/negative"
	if _, err := store.Create(draft); err == nil || !strings.Contains(err.Error(), "delay_ms must be zero or greater") {
		t.Fatalf("negative err = %v", err)
	}
	draft.DelayMS = int(MaxDelayMS) + 1
	draft.PathPattern = "/v1/overflow"
	if _, err := store.Create(draft); err == nil || !strings.Contains(err.Error(), "delay_ms is too large") {
		t.Fatalf("overflow err = %v", err)
	}
	draft.DelayMS = 0
	draft.PathPattern = "/v1/step"
	draft.Responses = []ResponseStep{{StatusCode: 200, DelayMS: -1}}
	if _, err := store.Create(draft); err == nil || !strings.Contains(err.Error(), "responses[0]") || !strings.Contains(err.Error(), "zero or greater") {
		t.Fatalf("step negative err = %v", err)
	}
	draft.Responses = []ResponseStep{{StatusCode: 200, DelayMS: int(MaxDelayMS) + 1}}
	if _, err := store.Create(draft); err == nil || !strings.Contains(err.Error(), "responses[0]") || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("step overflow err = %v", err)
	}
	draft.Responses = nil

	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	fileStore, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	zero := sampleDraft()
	zero.DelayMS = 0
	if _, err := fileStore.Create(zero); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "delay_ms") {
		t.Fatalf("zero delay was stored: %s", raw)
	}
	slow := sampleDraft()
	slow.PathPattern = "/v1/slow"
	slow.DelayMS = 120000
	created, err := fileStore.Create(slow)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"delay_ms": 120000`) {
		t.Fatalf("file = %s", raw)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(created.ID)
	if !ok || got.DelayMS != 120000 {
		t.Fatalf("reloaded = %+v ok=%v", got, ok)
	}

	legacy := filepath.Join(dir, "legacy.json")
	body := `{"rules":[{"id":"0123456789abcdef","name":"Old","enabled":true,"method":"GET","path_pattern":"/v1/old","status_code":200,"headers":{},"body":null,"created_at":"2026-11-05T12:00:00Z","updated_at":"2026-11-05T12:00:00Z"}]}`
	if err := os.WriteFile(legacy, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := Load(legacy)
	if err != nil {
		t.Fatal(err)
	}
	rule, ok := old.Match("GET", "/v1/old")
	if !ok || rule.DelayMS != 0 {
		t.Fatalf("legacy = %+v ok=%v", rule, ok)
	}

	broken := filepath.Join(dir, "broken.json")
	overflow := int64(math.MaxInt64)/int64(time.Millisecond) + 1
	body = `{"rules":[{"id":"0123456789abcdef","name":"Old","enabled":true,"method":"GET","path_pattern":"/v1/old","status_code":200,"delay_ms":` + strconv.FormatInt(overflow, 10) + `,"headers":{},"body":null,"created_at":"2026-11-05T12:00:00Z","updated_at":"2026-11-05T12:00:00Z"}]}`
	if err := os.WriteFile(broken, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(broken); err == nil || !strings.Contains(err.Error(), "delay_ms is too large") {
		t.Fatalf("broken file err = %v", err)
	}
	negative := filepath.Join(dir, "negative.json")
	body = `{"rules":[{"id":"0123456789abcdef","name":"Old","enabled":true,"method":"GET","path_pattern":"/v1/old","status_code":200,"delay_ms":-1,"headers":{},"body":null,"created_at":"2026-11-05T12:00:00Z","updated_at":"2026-11-05T12:00:00Z"}]}`
	if err := os.WriteFile(negative, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(negative); err == nil || !strings.Contains(err.Error(), "delay_ms must be zero or greater") {
		t.Fatalf("negative file err = %v", err)
	}
}
