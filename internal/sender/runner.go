package sender

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/oscs1024-pixel/Syslog-Tester/internal/domain"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/source"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/transport"
)

type State string

const (
	Idle      State = "idle"
	Running   State = "running"
	Paused    State = "paused"
	Completed State = "completed"
	Cancelled State = "cancelled"
	Failed    State = "failed"
)

type Stats struct {
	JobID       string        `json:"job_id"`
	State       State         `json:"state"`
	Target      domain.Target `json:"target"`
	Started     time.Time     `json:"started"`
	Finished    time.Time     `json:"finished,omitempty"`
	Attempted   int64         `json:"attempted"`
	Sent        int64         `json:"sent"`
	FailedCount int64         `json:"failed_count"`
	Bytes       int64         `json:"bytes"`
	LastError   string        `json:"last_error,omitempty"`
}
type Job struct {
	Target      domain.Target
	Source      source.Factory
	Loops       int
	Interval    time.Duration
	StopOnError bool
}
type Runner struct {
	mu     sync.Mutex
	stats  Stats
	cancel context.CancelFunc
	resume chan struct{}
	done   chan struct{}
}

func New() *Runner                { return &Runner{stats: Stats{State: Idle}} }
func (r *Runner) Snapshot() Stats { r.mu.Lock(); defer r.mu.Unlock(); return r.stats }
func (r *Runner) update(f func(*Stats), notify func(Stats)) {
	r.mu.Lock()
	f(&r.stats)
	snap := r.stats
	r.mu.Unlock()
	if notify != nil {
		notify(snap)
	}
}
func (r *Runner) Start(parent context.Context, job Job, notify func(Stats)) (<-chan struct{}, error) {
	t, e := job.Target.Normalize()
	if e != nil {
		return nil, e
	}
	if job.Source == nil || job.Loops < 1 || job.Loops > 10000 || job.Interval < 0 || job.Interval > time.Hour {
		return nil, errors.New("任务参数无效")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, fmt.Errorf("生成任务 ID: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	r.mu.Lock()
	if r.stats.State == Running || r.stats.State == Paused {
		r.mu.Unlock()
		cancel()
		return nil, errors.New("已有任务正在执行")
	}
	done := make(chan struct{})
	r.done = done
	r.cancel = cancel
	r.resume = nil
	r.stats = Stats{JobID: hex.EncodeToString(random[:]), State: Running, Target: t, Started: time.Now()}
	snap := r.stats
	r.mu.Unlock()
	if notify != nil {
		notify(snap)
	}
	// 单 worker + 流式读取形成天然背压；不为每条消息创建 goroutine。
	go func() { defer close(done); defer cancel(); r.execute(ctx, job, t, notify) }()
	return done, nil
}
func (r *Runner) Pause() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stats.State != Running {
		return errors.New("任务未运行")
	}
	r.resume = make(chan struct{})
	r.stats.State = Paused
	return nil
}
func (r *Runner) Resume() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stats.State != Paused {
		return errors.New("任务未暂停")
	}
	r.stats.State = Running
	close(r.resume)
	r.resume = nil
	return nil
}
func (r *Runner) Cancel() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (r *Runner) await(ctx context.Context) error {
	for {
		r.mu.Lock()
		ch := r.resume
		paused := r.stats.State == Paused
		r.mu.Unlock()
		if !paused {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}
func (r *Runner) finish(state State, err error, notify func(Stats)) {
	r.update(func(s *Stats) {
		s.State = state
		s.Finished = time.Now()
		if err != nil {
			s.LastError = err.Error()
		}
	}, notify)
	r.mu.Lock()
	r.cancel = nil
	r.resume = nil
	r.mu.Unlock()
}
func (r *Runner) execute(ctx context.Context, job Job, target domain.Target, notify func(Stats)) {
	conn, e := transport.Dial(ctx, target)
	if e != nil {
		if ctx.Err() != nil {
			r.finish(Cancelled, ctx.Err(), notify)
		} else {
			r.finish(Failed, e, notify)
		}
		return
	}
	defer conn.Close()
	var lastNotify time.Time
	for loop := 0; loop < job.Loops; loop++ {
		rd, err := job.Source()
		if err != nil {
			r.finish(Failed, fmt.Errorf("打开输入: %w", err), notify)
			return
		}
		for {
			if err := r.await(ctx); err != nil {
				_ = rd.Close()
				r.finish(Cancelled, err, notify)
				return
			}
			payload, err := rd.Next(ctx)
			if err == io.EOF {
				break
			}
			if err != nil {
				_ = rd.Close()
				r.finish(Failed, fmt.Errorf("读取日志: %w", err), notify)
				return
			}
			n, sendErr := conn.Send(ctx, payload)
			if ctx.Err() != nil {
				_ = rd.Close()
				r.finish(Cancelled, ctx.Err(), notify)
				return
			}
			r.mu.Lock()
			r.stats.Attempted++
			if sendErr != nil {
				r.stats.FailedCount++
				r.stats.LastError = sendErr.Error()
			} else {
				r.stats.Sent++
				r.stats.Bytes += int64(n)
			}
			snap := r.stats
			r.mu.Unlock()
			if notify != nil && time.Since(lastNotify) > 100*time.Millisecond {
				notify(snap)
				lastNotify = time.Now()
			}
			if sendErr != nil && job.StopOnError {
				_ = rd.Close()
				r.finish(Failed, sendErr, notify)
				return
			}
			if job.Interval > 0 {
				timer := time.NewTimer(job.Interval)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					_ = rd.Close()
					r.finish(Cancelled, ctx.Err(), notify)
					return
				case <-timer.C:
				}
			}
		}
		if err := rd.Close(); err != nil {
			r.finish(Failed, fmt.Errorf("关闭文件: %w", err), notify)
			return
		}
	}
	r.finish(Completed, nil, notify)
}
