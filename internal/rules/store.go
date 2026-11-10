package rules

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrNotFound means the id is not in the store.
var ErrNotFound = errors.New("rule not found")

// ErrDuplicate means another rule already uses this method and path pattern.
var ErrDuplicate = errors.New("a rule with this method and path pattern already exists")

// Store is the in-memory rule collection. The mutex guards rules.
// Match copies the winner and releases the lock before the caller writes a response.
type Store struct {
	mu    sync.RWMutex
	rules []Rule
	path  string
	now   func() time.Time
	newID func() (string, error)
}

// NewMemory returns an empty store that does not touch the filesystem.
func NewMemory() *Store {
	return newStore("")
}

type rulesFile struct {
	Rules []Rule `json:"rules"`
}

// Load reads rules from path. A missing file starts empty and is not created.
// Invalid JSON or an invalid rule returns an error and does not rewrite the file.
func Load(path string) (*Store, error) {
	store := newStore(path)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	var doc rulesFile
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("RULES_PATH %s contains invalid JSON: %w", path, err)
	}
	if doc.Rules == nil {
		doc.Rules = []Rule{}
	}
	seen := map[string]struct{}{}
	for i := range doc.Rules {
		body, err := compactStoredBody(doc.Rules[i].Body)
		if err != nil {
			return nil, fmt.Errorf("RULES_PATH %s: %w", path, err)
		}
		doc.Rules[i].Body = body
		rule := doc.Rules[i]
		if err := validateLoaded(rule, store.wildcards()); err != nil {
			return nil, fmt.Errorf("RULES_PATH %s: %w", path, err)
		}
		key := rule.Method + " " + rule.PathPattern + " " + rule.UpstreamID
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("RULES_PATH %s: duplicate rule %s %s", path, rule.Method, rule.PathPattern)
		}
		seen[key] = struct{}{}
	}
	store.rules = doc.Rules
	return store, nil
}

func newStore(path string) *Store {
	return &Store{
		path:  path,
		rules: []Rule{},
		now:   time.Now,
		newID: randomID,
	}
}

// Create validates a draft, rejects a duplicate method and pattern, and stores the rule.
func (s *Store) Create(d Draft) (Rule, error) {
	rule, err := normalize(d, s.wildcards())
	if err != nil {
		return Rule{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := s.uniqueID()
	if err != nil {
		return Rule{}, err
	}
	now := utc(s.now())
	rule.ID = id
	rule.CreatedAt = now
	rule.UpdatedAt = now
	if s.duplicate(rule.Method, rule.PathPattern, rule.UpstreamID, "") {
		return Rule{}, ErrDuplicate
	}
	next := append(append([]Rule{}, s.rules...), rule)
	if err := s.persist(next); err != nil {
		return Rule{}, err
	}
	s.rules = next
	return cloneRule(rule), nil
}

// Update replaces a rule. The id and created_at stay the same.
func (s *Store) Update(id string, d Draft) (Rule, error) {
	rule, err := normalize(d, s.wildcards())
	if err != nil {
		return Rule{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.index(id)
	if index < 0 {
		return Rule{}, ErrNotFound
	}
	rule.ID = id
	rule.CreatedAt = s.rules[index].CreatedAt
	rule.UpdatedAt = utc(s.now())
	if s.duplicate(rule.Method, rule.PathPattern, rule.UpstreamID, id) {
		return Rule{}, ErrDuplicate
	}
	next := append([]Rule{}, s.rules...)
	next[index] = rule
	if err := s.persist(next); err != nil {
		return Rule{}, err
	}
	s.rules = next
	return cloneRule(rule), nil
}

// Delete removes one rule.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.index(id)
	if index < 0 {
		return ErrNotFound
	}
	next := make([]Rule, 0, len(s.rules)-1)
	next = append(next, s.rules[:index]...)
	next = append(next, s.rules[index+1:]...)
	if err := s.persist(next); err != nil {
		return err
	}
	s.rules = next
	return nil
}

// Get returns a copy of one rule.
func (s *Store) Get(id string) (Rule, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index := s.index(id)
	if index < 0 {
		return Rule{}, false
	}
	return cloneRule(s.rules[index]), true
}

// List returns copies in creation order.
func (s *Store) List() []Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Rule, len(s.rules))
	for i := range s.rules {
		out[i] = cloneRule(s.rules[i])
	}
	return out
}

// Match returns a copy of the winning enabled rule for any upstream.
// The lock is released before return. The caller can write the response without holding it.
func (s *Store) Match(method, path string) (Rule, bool) {
	return s.MatchActive(method, path, "")
}

// MatchActive returns a copy of the winning enabled rule.
// A rule matches when its upstream id is empty or equal to activeID.
func (s *Store) MatchActive(method, path, activeID string) (Rule, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	method = strings.ToUpper(method)
	var (
		winner Rule
		found  bool
	)
	for i := range s.rules {
		rule := s.rules[i]
		if !rule.Enabled || !strings.EqualFold(rule.Method, method) {
			continue
		}
		if rule.UpstreamID != "" && rule.UpstreamID != activeID {
			continue
		}
		if !matchPattern(rule.PathPattern, path) {
			continue
		}
		if !found || better(rule, winner) {
			winner = rule
			found = true
		}
	}
	if !found {
		return Rule{}, false
	}
	return cloneRule(winner), true
}

// DetachUpstream clears upstream id on every rule that used it.
// Those rules become Any upstream. The change is refused when two of them
// would share a method and path with an existing Any rule.
func (s *Store) DetachUpstream(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make([]Rule, len(s.rules))
	changed := false
	for i := range s.rules {
		next[i] = cloneRule(s.rules[i])
		if next[i].UpstreamID == id {
			next[i].UpstreamID = ""
			next[i].UpdatedAt = utc(s.now())
			changed = true
		}
	}
	if !changed {
		return nil
	}
	seen := map[string]struct{}{}
	for _, rule := range next {
		key := rule.Method + " " + rule.PathPattern + " " + rule.UpstreamID
		if _, ok := seen[key]; ok {
			return fmt.Errorf("deleting this upstream would make a duplicate Any rule for %s %s", rule.Method, rule.PathPattern)
		}
		seen[key] = struct{}{}
	}
	if err := s.persist(next); err != nil {
		return err
	}
	s.rules = next
	return nil
}

func (s *Store) wildcards() bool {
	return true
}

func (s *Store) persist(rules []Rule) error {
	if s.path == "" {
		return nil
	}
	if rules == nil {
		rules = []Rule{}
	}
	dir := filepath.Dir(s.path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	payload, err := json.MarshalIndent(rulesFile{Rules: rules}, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	tmp := s.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	committed = true
	// The new file is already in place. A mode error must not roll memory back.
	_ = os.Chmod(s.path, 0o600)
	return nil
}

func compactStoredBody(body json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return nil, fmt.Errorf("mock body must be empty or JSON")
	}
	return json.RawMessage(buf.Bytes()), nil
}

func validateLoaded(rule Rule, allowWildcard bool) error {
	if _, err := hex.DecodeString(rule.ID); err != nil || len(rule.ID) != 16 {
		return fmt.Errorf("rule id %q is invalid", rule.ID)
	}
	if rule.CreatedAt.IsZero() || rule.UpdatedAt.IsZero() {
		return fmt.Errorf("rule %s is missing timestamps", rule.ID)
	}
	draft := Draft{
		Name:         rule.Name,
		Enabled:      rule.Enabled,
		FreshRequest: rule.FreshRequest,
		UpstreamID:   rule.UpstreamID,
		Method:       rule.Method,
		PathPattern:  rule.PathPattern,
		StatusCode:   rule.StatusCode,
		DelayMS:      rule.DelayMS,
		ResponseMode: rule.ResponseMode,
		RawResponse:  rule.RawResponse,
		MockHeader:   rule.MockHeader,
		Headers:      rule.Headers,
		Body:         string(rule.Body),
	}
	if len(rule.Body) == 0 {
		draft.Body = ""
	}
	_, err := normalize(draft, allowWildcard)
	return err
}

func (s *Store) index(id string) int {
	for i := range s.rules {
		if s.rules[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) duplicate(method, pattern, upstreamID, exceptID string) bool {
	for _, rule := range s.rules {
		if rule.ID == exceptID {
			continue
		}
		if rule.Method == method && rule.PathPattern == pattern && rule.UpstreamID == upstreamID {
			return true
		}
	}
	return false
}

func (s *Store) uniqueID() (string, error) {
	for i := 0; i < 5; i++ {
		id, err := s.newID()
		if err != nil {
			return "", err
		}
		if s.index(id) < 0 {
			return id, nil
		}
	}
	return "", errors.New("could not assign a rule id")
}

func randomID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
