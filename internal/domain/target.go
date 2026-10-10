package domain

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

type Protocol string

const (
	UDP Protocol = "udp"
	TCP Protocol = "tcp"
)

type Framing string

const (
	OctetCounting Framing = "octet-counting"
	Newline       Framing = "lf"
	CRLF          Framing = "crlf"
)

type Target struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Host           string        `json:"host"`
	Port           uint16        `json:"port"`
	Protocol       Protocol      `json:"protocol"`
	Framing        Framing       `json:"framing,omitempty"`
	LocalIP        string        `json:"local_ip,omitempty"`
	ConnectTimeout time.Duration `json:"connect_timeout"`
	WriteTimeout   time.Duration `json:"write_timeout"`
	Default        bool          `json:"default,omitempty"`
}

// Normalize 验证配置并返回独立副本；发送任务始终使用快照，不受编辑目标影响。
func (t Target) Normalize() (Target, error) {
	t.ID = strings.TrimSpace(t.ID)
	t.Name = strings.TrimSpace(t.Name)
	t.Host = strings.TrimSpace(t.Host)
	if t.Name == "" || len(t.Name) > 128 {
		return t, errors.New("目标名称长度必须为 1-128")
	}
	if t.Host == "" || strings.ContainsAny(t.Host, " \t\r\n/\\") {
		return t, errors.New("主机名或 IP 不合法")
	}
	if t.Port == 0 {
		return t, errors.New("端口不能为 0")
	}
	if t.Protocol != UDP && t.Protocol != TCP {
		return t, fmt.Errorf("不支持协议 %q", t.Protocol)
	}
	if t.Protocol == TCP {
		if t.Framing == "" {
			t.Framing = OctetCounting
		}
		if t.Framing != OctetCounting && t.Framing != Newline && t.Framing != CRLF {
			return t, errors.New("不支持 TCP 分帧")
		}
	} else {
		t.Framing = ""
	}
	if t.LocalIP != "" && net.ParseIP(t.LocalIP) == nil {
		return t, errors.New("绑定的源 IP 无效")
	}
	if t.ConnectTimeout == 0 {
		t.ConnectTimeout = 5 * time.Second
	}
	if t.WriteTimeout == 0 {
		t.WriteTimeout = 5 * time.Second
	}
	if t.ConnectTimeout < time.Millisecond || t.ConnectTimeout > time.Minute || t.WriteTimeout < time.Millisecond || t.WriteTimeout > time.Minute {
		return t, errors.New("超时必须介于 1ms 和 1min 之间")
	}
	return t, nil
}
func (t Target) Address() string { return net.JoinHostPort(t.Host, fmt.Sprint(t.Port)) }
