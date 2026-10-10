package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/oscs1024-pixel/Syslog-Tester/internal/domain"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/sender"
)

const version = 1
const maxHistory = 300

type Record struct {
	ID    string       `json:"id"`
	Stats sender.Stats `json:"stats"`
}
type Data struct {
	Version    int             `json:"version"`
	Targets    []domain.Target `json:"targets"`
	SelectedID string          `json:"selected_id,omitempty"`
	History    []Record        `json:"history"`
}
type Store struct {
	mu   sync.Mutex
	path string
	data Data
}

func DefaultPath() (string, error) {
	dir, e := os.UserConfigDir()
	if e != nil {
		return "", e
	}
	return filepath.Join(dir, "SyslogTester", "settings.json"), nil
}
func Open(path string) (*Store, error) {
	s := &Store{path: path, data: Data{Version: version}}
	b, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		return s, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(b, &s.data); e != nil {
		return nil, fmt.Errorf("读取配置: %w", e)
	}
	if s.data.Version != version {
		return nil, fmt.Errorf("不支持配置版本 %d", s.data.Version)
	}
	for _, t := range s.data.Targets {
		if _, e := t.Normalize(); e != nil {
			return nil, fmt.Errorf("损坏的目标 %q: %w", t.Name, e)
		}
	}
	return s, nil
}
func copyData(d Data) Data {
	d.Targets = slices.Clone(d.Targets)
	d.History = slices.Clone(d.History)
	return d
}
func (s *Store) Snapshot() Data { s.mu.Lock(); defer s.mu.Unlock(); return copyData(s.data) }
func id() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(b[:]), nil
}

// 修改在内存副本中完成；持久化成功后才发布，从而避免保存失败的假成功。
func (s *Store) mutate(apply func(*Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := copyData(s.data)
	if e := apply(&next); e != nil {
		return e
	}
	if e := atomicSave(s.path, next); e != nil {
		return e
	}
	s.data = next
	return nil
}
func (s *Store) Upsert(target domain.Target) (domain.Target, error) {
	t, e := target.Normalize()
	if e != nil {
		return t, e
	}
	if t.ID == "" {
		t.ID, e = id()
		if e != nil {
			return t, e
		}
	}
	e = s.mutate(func(d *Data) error {
		pos := -1
		for i, old := range d.Targets {
			if old.ID == t.ID {
				pos = i
				break
			}
		}
		if pos < 0 {
			d.Targets = append(d.Targets, t)
		} else {
			d.Targets[pos] = t
		}
		if len(d.Targets) == 1 {
			d.SelectedID = t.ID
		}
		if t.Default {
			for i := range d.Targets {
				d.Targets[i].Default = d.Targets[i].ID == t.ID
			}
			d.SelectedID = t.ID
		}
		return nil
	})
	return t, e
}
func (s *Store) Delete(targetID string) error {
	return s.mutate(func(d *Data) error {
		n := len(d.Targets)
		d.Targets = slices.DeleteFunc(d.Targets, func(t domain.Target) bool { return t.ID == targetID })
		if len(d.Targets) == n {
			return errors.New("目标不存在")
		}
		if d.SelectedID == targetID {
			d.SelectedID = ""
			if len(d.Targets) > 0 {
				d.SelectedID = d.Targets[0].ID
			}
		}
		return nil
	})
}
func (s *Store) Select(targetID string) error {
	return s.mutate(func(d *Data) error {
		for _, t := range d.Targets {
			if t.ID == targetID {
				d.SelectedID = targetID
				return nil
			}
		}
		return errors.New("目标不存在")
	})
}
func (s *Store) Append(stats sender.Stats) error {
	key := stats.JobID
	if key == "" {
		var err error
		key, err = id()
		if err != nil {
			return err
		}
	}
	return s.mutate(func(d *Data) error {
		for _, old := range d.History {
			if old.ID == key {
				return nil
			}
		}
		d.History = append([]Record{{ID: key, Stats: stats}}, d.History...)
		if len(d.History) > maxHistory {
			d.History = d.History[:maxHistory]
		}
		return nil
	})
}
func atomicSave(path string, d Data) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(d, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e := f.Chmod(0600); e != nil {
		_ = f.Close()
		return e
	}
	if _, e := f.Write(b); e != nil {
		_ = f.Close()
		return e
	}
	if e := f.Sync(); e != nil {
		_ = f.Close()
		return e
	}
	if e := f.Close(); e != nil {
		return e
	}
	if e := os.Rename(f.Name(), path); e != nil {
		return e
	}
	// 文件 fsync 后再同步目录，降低意外掉电丢失新配置的风险。
	dir, e := os.Open(filepath.Dir(path))
	if e == nil {
		defer dir.Close()
		_ = dir.Sync()
	}
	return nil
}

// MaxHistory 表示磁盘中保留的最近任务数量。
func MaxHistory() int { return maxHistory }
