// Package history keeps an in-memory log of proxy exchanges.
package history

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Source says where the response came from.
type Source string

const (
	SourceMock          Source = "mock"
	SourceUpstream      Source = "upstream"
	SourceUpstreamError Source = "upstream-error"
	SourceRejected      Source = "rejected"
)

// Entry is one recorded request and response. Bodies are previews.
type Entry struct {
	ID                    string
	StartedAt             time.Time
	Method                string
	Path                  string
	RawQuery              string
	RequestHeaders        http.Header
	RequestBody           string
	Status                int
	ResponseHeaders       http.Header
	ResponseBody          string
	Source                Source
	RuleID                string
	UpstreamName          string
	RequestBodyTruncated  bool
	ResponseBodyTruncated bool
}

// Log is safe for concurrent record, list, and delete calls.
// It does not read or write the rules file.
type Log struct {
	mu      sync.Mutex
	entries []Entry
	limit   int
	seq     uint64
	now     func() time.Time
	newID   func() (string, error)
}

// New returns an empty log. limit is the number of newest entries kept.
func New(limit int) *Log {
	if limit < 1 {
		limit = 500
	}
	return &Log{
		limit: limit,
		now:   time.Now,
		newID: randomID,
	}
}

// Add appends an entry and drops the oldest entries past the limit.
func (l *Log) Add(entry Entry) Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry.ID == "" {
		id, err := l.newID()
		if err != nil {
			l.seq++
			id = fmt.Sprintf("%016x", l.seq)
		}
		entry.ID = id
	}
	if entry.StartedAt.IsZero() {
		entry.StartedAt = l.now().UTC()
	} else {
		entry.StartedAt = entry.StartedAt.UTC()
	}
	entry.RequestHeaders = cloneHeader(entry.RequestHeaders)
	entry.ResponseHeaders = cloneHeader(entry.ResponseHeaders)
	l.entries = append(l.entries, entry)
	if len(l.entries) > l.limit {
		l.entries = append([]Entry(nil), l.entries[len(l.entries)-l.limit:]...)
	}
	return entry
}

// ParseSource reads a history filter query value.
// An empty or unknown value is not a source. The caller shows every entry.
func ParseSource(raw string) (Source, bool) {
	switch Source(strings.TrimSpace(raw)) {
	case SourceMock, SourceUpstream, SourceUpstreamError, SourceRejected:
		return Source(strings.TrimSpace(raw)), true
	default:
		return "", false
	}
}

// Filter keeps entries with source, in the same order.
func Filter(entries []Entry, source Source) []Entry {
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Source == source {
			out = append(out, entry)
		}
	}
	return out
}

// List returns copies, newest first.
func (l *Log) List() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, len(l.entries))
	for i := range l.entries {
		out[len(l.entries)-1-i] = copyEntry(l.entries[i])
	}
	return out
}

// Get returns a copy of one entry.
func (l *Log) Get(id string) (Entry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.entries {
		if l.entries[i].ID == id {
			return copyEntry(l.entries[i]), true
		}
	}
	return Entry{}, false
}

// Delete removes one entry. Other entries stay.
func (l *Log) Delete(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.entries {
		if l.entries[i].ID == id {
			l.entries = append(l.entries[:i], l.entries[i+1:]...)
			return true
		}
	}
	return false
}

// Clear removes every entry.
func (l *Log) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = nil
}

func copyEntry(entry Entry) Entry {
	entry.RequestHeaders = cloneHeader(entry.RequestHeaders)
	entry.ResponseHeaders = cloneHeader(entry.ResponseHeaders)
	return entry
}

func cloneHeader(header http.Header) http.Header {
	if header == nil {
		return http.Header{}
	}
	return header.Clone()
}

func randomID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
