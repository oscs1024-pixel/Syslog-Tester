package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"strconv"
	"time"

	"github.com/oscs1024-pixel/Syslog-Tester/internal/domain"
)

// Client 是单任务独占的连接，Send 不支持并发调用。
type Client struct {
	target domain.Target
	conn   net.Conn
}

func Dial(ctx context.Context, target domain.Target) (*Client, error) {
	t, err := target.Normalize()
	if err != nil {
		return nil, err
	}
	d := net.Dialer{Timeout: t.ConnectTimeout}
	if t.LocalIP != "" {
		ip := net.ParseIP(t.LocalIP)
		if t.Protocol == domain.UDP {
			d.LocalAddr = &net.UDPAddr{IP: ip}
		} else {
			d.LocalAddr = &net.TCPAddr{IP: ip}
		}
	}
	// 连接建立使用总超时预算及有限退避；不自动重试消息写入，避免重复告警。
	budgetCtx, cancel := context.WithTimeout(ctx, t.ConnectTimeout)
	defer cancel()
	attempts := 3
	if t.Protocol == domain.UDP {
		attempts = 1
	} // UDP connect 并不探测远端是否可达。
	conn, err := dialWithRetry(budgetCtx, attempts, func(callCtx context.Context) (net.Conn, error) {
		return d.DialContext(callCtx, string(t.Protocol), t.Address())
	})
	if err != nil {
		return nil, fmt.Errorf("连接 %s: %w", t.Address(), err)
	}
	return &Client{target: t, conn: conn}, nil
}

// dialWithRetry 至多尝试指定次数；退避含抖动并支持立即取消。
func dialWithRetry(ctx context.Context, attempts int, open func(context.Context) (net.Conn, error)) (net.Conn, error) {
	if attempts < 1 {
		return nil, errors.New("重试预算必须大于零")
	}
	var last error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		conn, err := open(ctx)
		if err == nil {
			return conn, nil
		}
		last = err
		if i+1 == attempts {
			break
		}
		wait := time.Duration(75*(1<<i)+rand.IntN(50)) * time.Millisecond
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("%d 次连接尝试失败: %w", attempts, last)
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Frame 确定每条 TCP Syslog 消息的边界，长度前缀按字节而不是字符计算。
func Frame(f domain.Framing, payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("不能发送空消息")
	}
	switch f {
	case domain.OctetCounting:
		return append([]byte(strconv.Itoa(len(payload))+" "), payload...), nil
	case domain.Newline, domain.CRLF:
		if bytes.ContainsAny(payload, "\r\n") {
			return nil, errors.New("按行 TCP 分帧不能发送包含换行的消息，请选择 octet-counting")
		}
		suffix := "\n"
		if f == domain.CRLF {
			suffix = "\r\n"
		}
		return append(append([]byte(nil), payload...), suffix...), nil
	default:
		return nil, fmt.Errorf("未知分帧类型 %q", f)
	}
}

func (c *Client) Send(ctx context.Context, payload []byte) (int, error) {
	if c == nil || c.conn == nil {
		return 0, errors.New("连接未打开")
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if len(payload) == 0 {
		return 0, errors.New("不能发送空消息")
	}
	wire := payload
	if c.target.Protocol == domain.UDP {
		// IPv4 的理论 UDP payload 上限；实际网络建议保持在 MTU 内。
		if len(payload) > 65507 {
			return 0, errors.New("UDP 消息超过 65507 字节")
		}
	} else {
		framed, err := Frame(c.target.Framing, payload)
		if err != nil {
			return 0, err
		}
		wire = framed
	}
	deadline := time.Now().Add(c.target.WriteTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		return 0, err
	}
	// 上层取消可立即解除阻塞的 Write；不直接关闭以免竞态。
	stop := context.AfterFunc(ctx, func() { _ = c.conn.SetWriteDeadline(time.Now()) })
	defer stop()
	written := 0
	for written < len(wire) {
		n, err := c.conn.Write(wire[written:])
		written += n
		if err != nil {
			return 0, fmt.Errorf("网络写入失败 (%d/%d 字节): %w", written, len(wire), err)
		}
		if n == 0 {
			return 0, io.ErrShortWrite
		}
		if c.target.Protocol == domain.UDP && n != len(wire) {
			return 0, io.ErrShortWrite
		}
	}
	return len(payload), nil
}
