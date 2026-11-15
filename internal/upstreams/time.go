package upstreams

import (
	"encoding/json"
	"time"
)

// timeValue stores UTC timestamps and encodes them as RFC3339.
type timeValue struct {
	time.Time
}

func utc(t time.Time) timeValue {
	return timeValue{Time: t.UTC().Truncate(time.Second)}
}

func (t timeValue) MarshalJSON() ([]byte, error) {
	if t.Time.IsZero() {
		return []byte(`""`), nil
	}
	stamp := t.Time.UTC().Format(time.RFC3339)
	return json.Marshal(stamp)
}

func (t *timeValue) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || string(data) == `""` {
		t.Time = time.Time{}
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return err
	}
	t.Time = parsed.UTC()
	return nil
}
