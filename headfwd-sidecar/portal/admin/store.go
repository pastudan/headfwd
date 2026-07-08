package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/headfwd/sidecar/portal/headscale"
)

type contextKey string

// ContextKeyUserName is the context key under which the resolved caller username
// is stored by the admin middleware. Handlers can read it to identify the caller.
const ContextKeyUserName contextKey = "admin_user_name"

// Store is an in-memory admin list backed by admins.json on disk.
// The first headscale user (by createdAt, excluding headfwd-server) is
// auto-promoted when the store is empty.
type Store struct {
	mu     sync.RWMutex
	path   string
	admins map[string]struct{}
}

// NewStore loads or initialises the admin store from stateDir/admins.json.
func NewStore(stateDir string) (*Store, error) {
	s := &Store{
		path:   filepath.Join(stateDir, "admins.json"),
		admins: make(map[string]struct{}),
	}
	if err := s.load(); err != nil {
		return nil, fmt.Errorf("admin store: %w", err)
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse admins.json: %w", err)
	}
	for _, name := range list {
		s.admins[name] = struct{}{}
	}
	return nil
}

// save must be called with the write lock held.
func (s *Store) save() error {
	list := s.unsortedList()
	sort.Strings(list)
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// unsortedList returns the admin names without acquiring the lock (caller holds it).
func (s *Store) unsortedList() []string {
	list := make([]string, 0, len(s.admins))
	for name := range s.admins {
		list = append(list, name)
	}
	return list
}

// IsAdmin reports whether name is an admin.
func (s *Store) IsAdmin(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.admins[name]
	return ok
}

// List returns a sorted slice of all admin names.
func (s *Store) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := s.unsortedList()
	sort.Strings(list)
	return list
}

// HasAny reports whether at least one admin exists.
func (s *Store) HasAny() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.admins) > 0
}

// Add promotes name to admin and persists the change.
func (s *Store) Add(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.admins[name] = struct{}{}
	return s.save()
}

// Remove revokes admin from name and persists the change.
// Returns an error if name is the last admin.
func (s *Store) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.admins) <= 1 {
		return errors.New("cannot remove the last admin")
	}
	delete(s.admins, name)
	return s.save()
}

// Bootstrap auto-promotes the oldest non-system headscale user if no admins
// exist yet. It is safe to call concurrently and is a no-op when admins already
// exist or when no eligible users are found.
func (s *Store) Bootstrap(hs *headscale.Client) error {
	if s.HasAny() {
		return nil
	}
	users, err := hs.ListUsers()
	if err != nil {
		return fmt.Errorf("bootstrap: list users: %w", err)
	}
	var oldest *headscale.User
	for i := range users {
		u := &users[i]
		if u.Name == "headfwd-server" {
			continue
		}
		if oldest == nil || u.CreatedAt < oldest.CreatedAt {
			oldest = u
		}
	}
	if oldest == nil {
		return nil
	}
	if err := s.Add(oldest.Name); err != nil {
		return fmt.Errorf("bootstrap: promote %s: %w", oldest.Name, err)
	}
	log.Printf("[admin] bootstrapped: %s promoted to admin", oldest.Name)
	return nil
}
