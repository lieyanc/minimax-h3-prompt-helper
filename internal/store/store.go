// Package store persists tasks as one JSON file each. There is no database:
// the whole dataset is a directory of readable JSON documents.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"h3helper/internal/task"
)

// Store is a directory of task JSON files, mirrored in memory.
type Store struct {
	dir   string
	mu    sync.RWMutex
	tasks map[string]*task.Task
}

// New opens (and creates) the task directory, loading everything it finds.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, tasks: map[string]*task.Task{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var t task.Task
		if err := json.Unmarshal(data, &t); err != nil {
			continue
		}
		if t.ID != "" {
			s.tasks[t.ID] = &t
		}
	}
	return s, nil
}

// NewID returns a sortable, collision-resistant task id.
func NewID() string {
	var b [5]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s-%s", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(b[:]))
}

// List returns every task summary, newest first.
func (s *Store) List() []task.Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]task.Summary, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t.Summarize())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// Get returns a copy of a task.
func (s *Store) Get(id string) (*task.Task, error) {
	s.mu.RLock()
	t, ok := s.tasks[id]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("任务 %s 不存在", id)
	}
	return clone(t)
}

// Create stores a new task.
func (s *Store) Create(t *task.Task) error {
	if t.ID == "" {
		t.ID = NewID()
	}
	now := time.Now()
	t.CreatedAt, t.UpdatedAt = now, now
	s.mu.Lock()
	s.tasks[t.ID] = t
	s.mu.Unlock()
	return s.persist(t)
}

// Update mutates a task under the write lock and persists the result. The
// returned task is a copy safe to serialise after the lock is released.
func (s *Store) Update(id string, fn func(*task.Task) error) (*task.Task, error) {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("任务 %s 不存在", id)
	}
	if err := fn(t); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	t.UpdatedAt = time.Now()
	copied, err := clone(t)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := s.persist(copied); err != nil {
		return nil, err
	}
	return copied, nil
}

// Delete removes a task and its file.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	_, ok := s.tasks[id]
	delete(s.tasks, id)
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("任务 %s 不存在", id)
	}
	err := os.Remove(s.path(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *Store) persist(t *task.Task) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(t.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(t.ID))
}

func clone(t *task.Task) (*task.Task, error) {
	data, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	var out task.Task
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
