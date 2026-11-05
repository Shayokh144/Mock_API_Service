package rules

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
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
	if s.duplicate(rule.Method, rule.PathPattern, "") {
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
	if s.duplicate(rule.Method, rule.PathPattern, id) {
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

// Match returns a copy of the winning enabled rule.
// The lock is released before return. The caller can write the response without holding it.
func (s *Store) Match(method, path string) (Rule, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	method = strings.ToUpper(method)
	for i := range s.rules {
		rule := s.rules[i]
		if !rule.Enabled || !strings.EqualFold(rule.Method, method) {
			continue
		}
		if rule.PathPattern == path {
			return cloneRule(rule), true
		}
	}
	return Rule{}, false
}

func (s *Store) wildcards() bool {
	return false
}

func (s *Store) persist(rules []Rule) error {
	if s.path == "" {
		return nil
	}
	return errors.New("rule file persistence is not available")
}

func (s *Store) index(id string) int {
	for i := range s.rules {
		if s.rules[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) duplicate(method, pattern, exceptID string) bool {
	for _, rule := range s.rules {
		if rule.ID == exceptID {
			continue
		}
		if rule.Method == method && rule.PathPattern == pattern {
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
