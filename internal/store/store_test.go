package store

import (
	"github.com/oscs1024-pixel/Syslog-Tester/internal/domain"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/sender"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestStoreRoundTripAndIdempotency(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "config.json")
	s, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	target, e := s.Upsert(domain.Target{Name: "SOC", Host: "127.0.0.1", Port: 514, Protocol: domain.UDP})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Upsert(target)
	if e != nil {
		t.Fatal(e)
	}
	snapshot := s.Snapshot()
	if len(snapshot.Targets) != 1 || snapshot.SelectedID != target.ID {
		t.Fatalf("%+v", snapshot)
	}
	snapshot.Targets[0].Name = "hacked"
	if s.Snapshot().Targets[0].Name == "hacked" {
		t.Fatal("shared mutable data")
	}
	s2, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	if s2.Snapshot().Targets[0].Name != "SOC" {
		t.Fatal("roundtrip")
	}
	if e := s2.Delete(target.ID); e != nil {
		t.Fatal(e)
	}
	if len(s2.Snapshot().Targets) != 0 {
		t.Fatal("delete")
	}
	if info, e := os.Stat(p); e != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions %v %v", info, e)
	}
}
func TestConcurrentHistory(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db.json"))
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 35; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.Append(sender.Stats{Sent: 1}); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if len(s.Snapshot().History) != 35 {
		t.Fatal("lost update")
	}
}
func TestSaveFailureKeepsMemory(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "missing", "db.json"))
	s.path = "/dev/null/invalid.json"
	if _, e := s.Upsert(domain.Target{Name: "x", Host: "localhost", Port: 514, Protocol: domain.UDP}); e == nil {
		t.Fatal("should fail")
	}
	if len(s.Snapshot().Targets) != 0 {
		t.Fatal("committed on failure")
	}
}

func TestAppendSameJobIsIdempotent(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "db.json"))
	stats := sender.Stats{JobID: "fixed-job-id", Sent: 5}
	if err := s.Append(stats); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(stats); err != nil {
		t.Fatal(err)
	}
	data := s.Snapshot()
	if len(data.History) != 1 || data.History[0].ID != stats.JobID {
		t.Fatalf("not idempotent: %+v", data.History)
	}
}
