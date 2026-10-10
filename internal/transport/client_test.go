package transport

import (
	"bytes"
	"context"
	"github.com/oscs1024-pixel/Syslog-Tester/internal/domain"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFrame(t *testing.T) {
	b := []byte("你好")
	f, e := Frame(domain.OctetCounting, b)
	if e != nil || string(f) != strconv.Itoa(len(b))+" 你好" {
		t.Fatalf("%q %v", f, e)
	}
	if _, e := Frame(domain.Newline, []byte("a\nb")); e == nil {
		t.Fatal("newline injection accepted")
	}
	f, e = Frame(domain.CRLF, []byte("hi"))
	if e != nil || string(f) != "hi\r\n" {
		t.Fatal("crlf")
	}
}
func TestUDPDatagram(t *testing.T) {
	l, e := net.ListenPacket("udp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	c, e := Dial(context.Background(), domain.Target{Name: "test", Host: "127.0.0.1", Port: uint16(l.LocalAddr().(*net.UDPAddr).Port), Protocol: domain.UDP})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	for _, m := range []string{"first", "second"} {
		if _, e := c.Send(context.Background(), []byte(m)); e != nil {
			t.Fatal(e)
		}
		buf := make([]byte, 200)
		_ = l.SetReadDeadline(time.Now().Add(time.Second))
		n, _, e := l.ReadFrom(buf)
		if e != nil || string(buf[:n]) != m {
			t.Fatalf("got %q %v", buf[:n], e)
		}
	}
	if _, e := c.Send(context.Background(), bytes.Repeat([]byte("x"), 65508)); e == nil {
		t.Fatal("accepted huge udp")
	}
}
func TestTCPFraming(t *testing.T) {
	for _, tc := range []struct {
		fmt    domain.Framing
		expect string
	}{{domain.OctetCounting, "5 hello"}, {domain.Newline, "hello\n"}, {domain.CRLF, "hello\r\n"}} {
		t.Run(string(tc.fmt), func(t *testing.T) {
			l, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			done := make(chan []byte, 1)
			go func() {
				conn, e := l.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				data := make([]byte, len(tc.expect))
				_, _ = io.ReadFull(conn, data)
				done <- data
			}()
			c, e := Dial(context.Background(), domain.Target{Name: "tcp", Host: "127.0.0.1", Port: uint16(l.Addr().(*net.TCPAddr).Port), Protocol: domain.TCP, Framing: tc.fmt})
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			if _, e := c.Send(context.Background(), []byte("hello")); e != nil {
				t.Fatal(e)
			}
			select {
			case got := <-done:
				if string(got) != tc.expect {
					t.Fatalf("%q", got)
				}
			case <-time.After(time.Second):
				t.Fatal("timeout")
			}
		})
	}
}
func TestCancelledDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := Dial(ctx, domain.Target{Name: "t", Host: "localhost", Port: 514, Protocol: domain.TCP})
	if e == nil || !strings.Contains(e.Error(), "canceled") {
		t.Fatal(e)
	}
}

func TestDialRetryBudget(t *testing.T) {
	attempts := 0
	conn, err := dialWithRetry(context.Background(), 3, func(context.Context) (net.Conn, error) {
		attempts++
		if attempts < 3 {
			return nil, io.ErrUnexpectedEOF
		}
		c, peer := net.Pipe()
		t.Cleanup(func() { _ = peer.Close() })
		return c, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if attempts != 3 {
		t.Fatalf("attempts=%d", attempts)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts = 0
	_, err = dialWithRetry(ctx, 3, func(context.Context) (net.Conn, error) { attempts++; return nil, io.ErrUnexpectedEOF })
	if err != context.Canceled || attempts != 0 {
		t.Fatalf("cancel %v attempts=%d", err, attempts)
	}
}
