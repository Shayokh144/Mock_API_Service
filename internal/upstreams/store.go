// Package upstreams stores named base URLs, each with a unique path prefix.
package upstreams

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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

// Upstream is one saved base URL.
type Upstream struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	BaseURL   string    `json:"base_url"`
	Prefix    string    `json:"prefix,omitempty"`
	CreatedAt timeValue `json:"created_at"`
	UpdatedAt timeValue `json:"updated_at"`
}

// AssignedPrefix is a prefix filled in while loading an older file.
type AssignedPrefix struct {
	Name   string
	Prefix string
}

// Route is the upstream selected for one request path.
type Route struct {
	Upstream      Upstream
	ForwardedPath string
	Prefixed      bool
	ViaReferer    bool
	ViaCookie     bool
}

// Store is the in-memory upstream collection. The mutex guards items.
type Store struct {
	mu       sync.RWMutex
	items    []Upstream
	assigned []AssignedPrefix
	path     string
	now      func() time.Time
	newID    func() (string, error)
}

type fileDoc struct {
	// ActiveID is read from older files and ignored. New files omit it.
	ActiveID  string     `json:"active_id,omitempty"`
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
	taken := map[string]struct{}{}
	type slot struct {
		src      Upstream
		prefix   string
		assigned bool
	}
	slots := make([]slot, len(doc.Upstreams))
	for i, src := range doc.Upstreams {
		slots[i].src = src
		prefix := strings.TrimSpace(src.Prefix)
		if prefix == "" {
			continue
		}
		norm, err := normalizePrefix(prefix)
		if err != nil {
			return nil, fmt.Errorf("upstreams file %s: %w", path, err)
		}
		if _, ok := taken[norm]; ok {
			return nil, fmt.Errorf("upstreams file %s: duplicate upstream prefix %s", path, norm)
		}
		taken[norm] = struct{}{}
		slots[i].prefix = norm
	}
	for i := range slots {
		if slots[i].prefix != "" {
			continue
		}
		prefix, err := assignPrefix(slots[i].src.Name, taken)
		if err != nil {
			return nil, fmt.Errorf("upstreams file %s: %w", path, err)
		}
		taken[prefix] = struct{}{}
		slots[i].prefix = prefix
		slots[i].assigned = true
	}
	items := make([]Upstream, 0, len(slots))
	var assigned []AssignedPrefix
	for _, slot := range slots {
		item, err := normalize(slot.src.Name, slot.src.BaseURL, slot.prefix)
		if err != nil {
			return nil, fmt.Errorf("upstreams file %s: %w", path, err)
		}
		if _, err := hex.DecodeString(slot.src.ID); err != nil || len(slot.src.ID) != 16 {
			return nil, fmt.Errorf("upstreams file %s: upstream id %q is invalid", path, slot.src.ID)
		}
		if slot.src.CreatedAt.IsZero() || slot.src.UpdatedAt.IsZero() {
			return nil, fmt.Errorf("upstreams file %s: upstream %s is missing timestamps", path, slot.src.ID)
		}
		item.ID = slot.src.ID
		item.CreatedAt = slot.src.CreatedAt
		item.UpdatedAt = slot.src.UpdatedAt
		if _, ok := ids[item.ID]; ok {
			return nil, fmt.Errorf("upstreams file %s: duplicate upstream id %s", path, item.ID)
		}
		if _, ok := names[item.Name]; ok {
			return nil, fmt.Errorf("upstreams file %s: duplicate upstream name %s", path, item.Name)
		}
		ids[item.ID] = struct{}{}
		names[item.Name] = struct{}{}
		if slot.assigned {
			assigned = append(assigned, AssignedPrefix{Name: item.Name, Prefix: item.Prefix})
		}
		items = append(items, item)
	}
	store.items = items
	store.assigned = assigned
	if len(assigned) > 0 {
		if err := store.persist(items); err != nil {
			return nil, err
		}
	}
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

// Seed adds rawURL as the only upstream when the store is empty.
// prefix is that upstream's path prefix. An empty prefix uses /app.
// A store that already has upstreams is left unchanged. An empty rawURL does nothing.
func (s *Store) Seed(rawURL, prefix string) error {
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
	if strings.TrimSpace(prefix) == "" {
		prefix = "/app"
	}
	name := parsed.Hostname()
	if name == "" {
		name = "default"
	}
	return s.createLocked(name, parsed.String(), prefix)
}

// AssignedPrefixes returns prefixes filled in from upstream names while loading.
func (s *Store) AssignedPrefixes() []AssignedPrefix {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AssignedPrefix, len(s.assigned))
	copy(out, s.assigned)
	return out
}

// PrefixWarning is the startup line for an upstream that had no prefix.
// listenAddr is the proxy address, such as 127.0.0.1:8080.
func PrefixWarning(name, prefix, listenAddr string) string {
	return fmt.Sprintf("warning: upstream %s had no prefix; assigned %s. App base URL is http://%s%s", name, prefix, appHost(listenAddr), prefix)
}

func appHost(listenAddr string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil || host == "" || port == "" {
		return "127.0.0.1:8080"
	}
	return net.JoinHostPort(host, port)
}

// Create validates a name, base URL, and prefix, then stores the upstream.
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
	if err := s.persist(next); err != nil {
		return err
	}
	s.items = next
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
	if err := s.persist(next); err != nil {
		return Upstream{}, err
	}
	s.items = next
	return item, nil
}

// Delete removes one upstream.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.index(id)
	if index < 0 {
		return ErrNotFound
	}
	next := make([]Upstream, 0, len(s.items)-1)
	next = append(next, s.items[:index]...)
	next = append(next, s.items[index+1:]...)
	if err := s.persist(next); err != nil {
		return err
	}
	s.items = next
	return nil
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

// Route selects the upstream whose prefix matches path.
// A prefix matches the whole path or a following slash, and that prefix is removed.
// A path that matches no prefix is not routed.
func (s *Store) Route(path string) (Route, bool) {
	if path == "" {
		path = "/"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.routePrefix(path)
}

func (s *Store) routePrefix(path string) (Route, bool) {
	for i := range s.items {
		item := s.items[i]
		forwarded, ok := stripPrefix(path, item.Prefix)
		if !ok {
			continue
		}
		return Route{Upstream: item, ForwardedPath: forwarded, Prefixed: true}, true
	}
	return Route{}, false
}

// MatchReferer routes requestPath to the upstream whose prefix matches the
// Referer path. The host and port are ignored. requestPath is not rewritten.
// A missing, malformed, or non-matching Referer does not match.
func (s *Store) MatchReferer(requestPath, referer string) (Route, bool) {
	if requestPath == "" {
		requestPath = "/"
	}
	refPath, ok := refererPath(referer)
	if !ok {
		return Route{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	route, ok := s.routePrefix(refPath)
	if !ok {
		return Route{}, false
	}
	route.ForwardedPath = requestPath
	route.Prefixed = false
	route.ViaReferer = true
	return route, true
}

// MatchID routes requestPath, unchanged, to the upstream with id.
// It is used for the sticky upstream cookie. An unknown id does not match.
func (s *Store) MatchID(requestPath, id string) (Route, bool) {
	if requestPath == "" {
		requestPath = "/"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	index := s.index(id)
	if id == "" || index < 0 {
		return Route{}, false
	}
	return Route{Upstream: s.items[index], ForwardedPath: requestPath, ViaCookie: true}, true
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
		return "", fmt.Errorf("prefix is required")
	}
	if strings.HasPrefix(raw, "/_admin") {
		return "", fmt.Errorf("prefix must not use /_admin")
	}
	if !strings.HasPrefix(raw, "/") || strings.HasSuffix(raw, "/") || strings.Count(raw, "/") != 1 {
		return "", fmt.Errorf("prefix must be one path segment, such as /shop")
	}
	segment := raw[1:]
	if segment == "" {
		return "", fmt.Errorf("prefix must be one path segment, such as /shop")
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
	if parsed.User != nil {
		return nil, fmt.Errorf("base URL must not include a username or password")
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

func (s *Store) persist(items []Upstream) error {
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
	payload, err := json.MarshalIndent(fileDoc{Upstreams: items}, "", "  ")
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

func refererPath(referer string) (string, bool) {
	referer = strings.TrimSpace(referer)
	if referer == "" {
		return "", false
	}
	parsed, err := url.Parse(referer)
	if err != nil || parsed.Path == "" || !strings.HasPrefix(parsed.Path, "/") {
		return "", false
	}
	return parsed.Path, true
}

func prefixFromName(name string) string {
	var b strings.Builder
	b.WriteByte('/')
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	return b.String()
}

func assignPrefix(name string, taken map[string]struct{}) (string, error) {
	base := prefixFromName(name)
	if base == "/" || strings.HasPrefix(base, "/_admin") {
		base = "/upstream"
	}
	for n := 1; n <= 10000; n++ {
		candidate := base
		if n > 1 {
			candidate = fmt.Sprintf("%s-%d", base, n)
		}
		if _, ok := taken[candidate]; ok {
			continue
		}
		if _, err := normalizePrefix(candidate); err != nil {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("could not assign a prefix for upstream %s", strings.TrimSpace(name))
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
