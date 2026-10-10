package sender

import (
	"context"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/domain"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/source"
	"net"
	"strings"
	"testing"
	"time"
)

func target(t *testing.T) (domain.Target, func()) {
	t.Helper()
	l, e := net.ListenPacket("udp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	return domain.Target{Name: "test", Host: "127.0.0.1", Port: uint16(l.LocalAddr().(*net.UDPAddr).Port), Protocol: domain.UDP}, func() { _ = l.Close() }
}
func TestRunnerCompletes(t *testing.T) {
	tgt, closeFn := target(t)
	defer closeFn()
	r := New()
	done, e := r.Start(context.Background(), Job{Target: tgt, Source: source.Single([]byte("log")), Loops: 3}, nil)
	if e != nil {
		t.Fatal(e)
	}
	<-done
	s := r.Snapshot()
	if s.State != Completed || s.Sent != 3 || s.Bytes != 9 {
		t.Fatalf("%+v", s)
	}
}
func TestRunnerCancellationAndPause(t *testing.T) {
	tgt, closeFn := target(t)
	defer closeFn()
	r := New()
	done, e := r.Start(context.Background(), Job{Target: tgt, Source: source.Single([]byte("log")), Loops: 200, Interval: time.Second}, nil)
	if e != nil {
		t.Fatal(e)
	}
	time.Sleep(30 * time.Millisecond)
	if e := r.Pause(); e != nil {
		t.Fatal(e)
	}
	if e := r.Resume(); e != nil {
		t.Fatal(e)
	}
	r.Cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel hangs")
	}
	if s := r.Snapshot(); s.State != Cancelled {
		t.Fatal(s)
	}
}
func TestRunnerRejectsBadJob(t *testing.T) {
	r := New()
	_, e := r.Start(context.Background(), Job{Source: source.Single([]byte("a"))}, nil)
	if e == nil {
		t.Fatal("invalid target")
	}
	if strings.TrimSpace(r.Snapshot().LastError) != "" {
		t.Fatal("polluted state")
	}
}
