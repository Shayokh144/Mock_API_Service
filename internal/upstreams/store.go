// Package upstreams stores named base URLs and the one active target.
package upstreams

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ErrNotFound means the id is not in the store.
var ErrNotFound = errors.New("upstream not found")

// ErrActive means the upstream is the active one and cannot be deleted yet.
var ErrActive = errors.New("cannot delete the fallback upstream; set another as fallback first")

// Upstream is one saved base URL.
type Upstream struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	BaseURL   string    `json:"base_url"`
	Prefix    string    `json:"prefix,omitempty"`
	CreatedAt timeValue `json:"created_at"`
	UpdatedAt timeValue `json:"updated_at"`
}

// Route is the upstream selected for one request path.
type Route struct {
	Upstream      Upstream
	ForwardedPath string
	Prefixed      bool
}

// Store is the in-memory upstream collection. The mutex guards items and activeID.
type Store struct {
	mu       sync.RWMutex
	items    []Upstream
	activeID string
	path     string
	now      func() time.Time
	newID    func() (string, error)
}

type fileDoc struct {
	ActiveID  string     `json:"active_id"`
	Upstreams []Upstream `json:"upstreams"`
}

// NewMemory returns an empty store that does not touch the filesystem.
func NewMemory() *Store {
	return newStore("")
}

// Load reads upstreams from path. A missing file starts empty and is not created.
// Invalid JSON or an invalid upstream returns an error and does not rewrite the file.
func Load(path string) (*Store, error) {
	store := newStore(path)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	var doc fileDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("upstreams file %s contains invalid JSON: %w", path, err)
	}
	if doc.Upstreams == nil {
		doc.Upstreams = []Upstream{}
	}
	names := map[string]struct{}{}
	ids := map[string]struct{}{}
	prefixes := map[string]struct{}{}
	for i := range doc.Upstreams {
		item, err := normalize(doc.Upstreams[i].Name, doc.Upstreams[i].BaseURL, doc.Upstreams[i].Prefix)
		if err != nil {
			return nil, fmt.Errorf("upstreams file %s: %w", path, err)
		}
		if _, err := hex.DecodeString(doc.Upstreams[i].ID); err != nil || len(doc.Upstreams[i].ID) != 16 {
			return nil, fmt.Errorf("upstreams file %s: upstream id %q is invalid", path, doc.Upstreams[i].ID)
		}
		if doc.Upstreams[i].CreatedAt.IsZero() || doc.Upstreams[i].UpdatedAt.IsZero() {
			return nil, fmt.Errorf("upstreams file %s: upstream %s is missing timestamps", path, doc.Upstreams[i].ID)
		}
		item.ID = doc.Upstreams[i].ID
		item.CreatedAt = doc.Upstreams[i].CreatedAt
		item.UpdatedAt = doc.Upstreams[i].UpdatedAt
		if _, ok := ids[item.ID]; ok {
			return nil, fmt.Errorf("upstreams file %s: duplicate upstream id %s", path, item.ID)
		}
		if _, ok := names[item.Name]; ok {
			return nil, fmt.Errorf("upstreams file %s: duplicate upstream name %s", path, item.Name)
		}
		if item.Prefix != "" {
			if _, ok := prefixes[item.Prefix]; ok {
				return nil, fmt.Errorf("upstreams file %s: duplicate upstream prefix %s", path, item.Prefix)
			}
			prefixes[item.Prefix] = struct{}{}
		}
		ids[item.ID] = struct{}{}
		names[item.Name] = struct{}{}
		doc.Upstreams[i] = item
	}
	if len(doc.Upstreams) == 0 {
		if doc.ActiveID != "" {
			return nil, fmt.Errorf("upstreams file %s: active_id is set but there are no upstreams", path)
		}
		return store, nil
	}
	if _, ok := ids[doc.ActiveID]; !ok {
		return nil, fmt.Errorf("upstreams file %s: active_id does not match an upstream", path)
	}
	store.items = doc.Upstreams
	store.activeID = doc.ActiveID
	return store, nil
}

func newStore(path string) *Store {
	return &Store{
		path:  path,
		items: []Upstream{},
		now:   time.Now,
		newID: randomID,
	}
}

// Seed adds rawURL as the only upstream, and the active one, when the store is empty.
// A store that already has upstreams is left unchanged. An empty rawURL does nothing.
func (s *Store) Seed(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) > 0 {
		return nil
	}
	parsed, err := parseBaseURL(rawURL)
	if err != nil {
		return err
	}
	name := parsed.Hostname()
	if name == "" {
		name = "default"
	}
	return s.createLocked(name, parsed.String(), "")
}

// Create validates a name, base URL, and optional prefix, then stores the upstream.
// The first upstream becomes active. Later ones do not change the active id.
func (s *Store) Create(name, baseURL, prefix string) (Upstream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.createLocked(name, baseURL, prefix); err != nil {
		return Upstream{}, err
	}
	return s.items[len(s.items)-1], nil
}

func (s *Store) createLocked(name, baseURL, prefix string) error {
	item, err := normalize(name, baseURL, prefix)
	if err != nil {
		return err
	}
	if s.nameTaken(item.Name, "") {
		return fmt.Errorf("an upstream named %q already exists", item.Name)
	}
	if s.prefixTaken(item.Prefix, "") {
		return fmt.Errorf("an upstream already uses prefix %s", item.Prefix)
	}
	id, err := s.uniqueID()
	if err != nil {
		return err
	}
	now := utc(s.now())
	item.ID = id
	item.CreatedAt = now
	item.UpdatedAt = now
	next := append(append([]Upstream{}, s.items...), item)
	active := s.activeID
	if active == "" {
		active = item.ID
	}
	if err := s.persist(next, active); err != nil {
		return err
	}
	s.items = next
	s.activeID = active
	return nil
}

// Update replaces the name, base URL, and prefix. The id and created_at stay the same.
func (s *Store) Update(id, name, baseURL, prefix string) (Upstream, error) {
	item, err := normalize(name, baseURL, prefix)
	if err != nil {
		return Upstream{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.index(id)
	if index < 0 {
		return Upstream{}, ErrNotFound
	}
	if s.nameTaken(item.Name, id) {
		return Upstream{}, fmt.Errorf("an upstream named %q already exists", item.Name)
	}
	if s.prefixTaken(item.Prefix, id) {
		return Upstream{}, fmt.Errorf("an upstream already uses prefix %s", item.Prefix)
	}
	item.ID = id
	item.CreatedAt = s.items[index].CreatedAt
	item.UpdatedAt = utc(s.now())
	next := append([]Upstream{}, s.items...)
	next[index] = item
	if err := s.persist(next, s.activeID); err != nil {
		return Upstream{}, err
	}
	s.items = next
	return item, nil
}

// Delete removes one upstream. The active upstream is kept until another is active.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.index(id)
	if index < 0 {
		return ErrNotFound
	}
	if s.activeID == id {
		return ErrActive
	}
	next := make([]Upstream, 0, len(s.items)-1)
	next = append(next, s.items[:index]...)
	next = append(next, s.items[index+1:]...)
	if err := s.persist(next, s.activeID); err != nil {
		return err
	}
	s.items = next
	return nil
}

// ClearActiveForTest removes the fallback so a non-matching path has no upstream.
// Prefix matches still route. The dashboard cannot save this state.
func (s *Store) ClearActiveForTest() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeID = ""
}

// Activate makes id the fallback for requests that match no prefix.
func (s *Store) Activate(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index(id) < 0 {
		return ErrNotFound
	}
	if s.activeID == id {
		return nil
	}
	if err := s.persist(s.items, id); err != nil {
		return err
	}
	s.activeID = id
	return nil
}

// Active returns a copy of the active upstream.
func (s *Store) Active() (Upstream, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index := s.index(s.activeID)
	if index < 0 {
		return Upstream{}, false
	}
	return s.items[index], true
}

// Get returns a copy of one upstream.
func (s *Store) Get(id string) (Upstream, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index := s.index(id)
	if index < 0 {
		return Upstream{}, false
	}
	return s.items[index], true
}

// Route selects the upstream for path.
// A prefix matches the whole path or a following slash, and that prefix is removed.
// Otherwise the active upstream is the fallback and the path stays unchanged.
func (s *Store) Route(path string) (Route, bool) {
	if path == "" {
		path = "/"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.items {
		item := s.items[i]
		forwarded, ok := stripPrefix(path, item.Prefix)
		if !ok {
			continue
		}
		return Route{Upstream: item, ForwardedPath: forwarded, Prefixed: true}, true
	}
	index := s.index(s.activeID)
	if index < 0 {
		return Route{}, false
	}
	return Route{Upstream: s.items[index], ForwardedPath: path}, true
}

// List returns copies in creation order.
func (s *Store) List() []Upstream {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Upstream, len(s.items))
	copy(out, s.items)
	return out
}

func stripPrefix(path, prefix string) (string, bool) {
	if prefix == "" {
		return "", false
	}
	if path == prefix {
		return "/", true
	}
	if strings.HasPrefix(path, prefix+"/") {
		return path[len(prefix):], true
	}
	return "", false
}

func normalizePrefix(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "/_admin") {
		return "", fmt.Errorf("prefix must not use /_admin")
	}
	if !strings.HasPrefix(raw, "/") || strings.HasSuffix(raw, "/") || strings.Count(raw, "/") != 1 {
		return "", fmt.Errorf("prefix must be one path segment, such as /okya")
	}
	segment := raw[1:]
	if segment == "" {
		return "", fmt.Errorf("prefix must be one path segment, such as /okya")
	}
	for _, r := range segment {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return "", fmt.Errorf("prefix must use only letters, digits, - and _")
		}
	}
	return raw, nil
}

func normalize(name, baseURL, prefix string) (Upstream, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Upstream{}, fmt.Errorf("name is required")
	}
	if utf8.RuneCountInString(name) > 80 {
		return Upstream{}, fmt.Errorf("name must be 80 characters or fewer")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return Upstream{}, fmt.Errorf("name contains a control character")
		}
	}
	parsed, err := parseBaseURL(baseURL)
	if err != nil {
		return Upstream{}, err
	}
	prefix, err = normalizePrefix(prefix)
	if err != nil {
		return Upstream{}, err
	}
	return Upstream{Name: name, BaseURL: parsed.String(), Prefix: prefix}, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Scheme == "" {
		return nil, fmt.Errorf("base URL must be an absolute http or https URL")
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("base URL must include a host")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("base URL must be http or https")
	}
	if parsed.RawQuery != "" {
		return nil, fmt.Errorf("base URL must not include a query")
	}
	if parsed.Fragment != "" {
		return nil, fmt.Errorf("base URL must not include a fragment")
	}
	parsed.Scheme = scheme
	return parsed, nil
}

func (s *Store) persist(items []Upstream, activeID string) error {
	if s.path == "" {
		return nil
	}
	if items == nil {
		items = []Upstream{}
	}
	dir := filepath.Dir(s.path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	payload, err := json.MarshalIndent(fileDoc{ActiveID: activeID, Upstreams: items}, "", "  ")
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
	_ = os.Chmod(s.path, 0o600)
	return nil
}

func (s *Store) index(id string) int {
	for i := range s.items {
		if s.items[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) prefixTaken(prefix, exceptID string) bool {
	if prefix == "" {
		return false
	}
	for _, item := range s.items {
		if item.ID == exceptID {
			continue
		}
		if item.Prefix == prefix {
			return true
		}
	}
	return false
}

func (s *Store) nameTaken(name, exceptID string) bool {
	for _, item := range s.items {
		if item.ID == exceptID {
			continue
		}
		if item.Name == name {
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
	return "", errors.New("could not assign an upstream id")
}

func randomID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
