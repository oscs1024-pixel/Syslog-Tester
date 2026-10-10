package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
	conn, err := d.DialContext(ctx, string(t.Protocol), t.Address())
	if err != nil {
		return nil, fmt.Errorf("连接 %s: %w", t.Address(), err)
	}
	return &Client{target: t, conn: conn}, nil
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
		return append(append([]byte(strconv.Itoa(len(payload))+" "), payload...), []byte{}...), nil
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
