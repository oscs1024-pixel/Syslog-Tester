package syslog

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Format string

const (
	Raw     Format = "raw"
	RFC3164 Format = "rfc3164"
	RFC5424 Format = "rfc5424"
)

type Fields struct {
	Facility  uint8
	Severity  uint8
	Timestamp time.Time
	Hostname  string
	AppName   string
	ProcID    string
	MsgID     string
	Message   string
}

func (f Fields) validate() error {
	if f.Facility > 23 || f.Severity > 7 {
		return errors.New("facility 必须在 0-23，severity 必须在 0-7")
	}
	if strings.ContainsAny(f.Message, "\x00") {
		return errors.New("消息不能包含 NUL")
	}
	return nil
}

var printableASCII = regexp.MustCompile(`^[\x21-\x7e]{1,48}$`)

func field(s string) (string, error) {
	if s == "" {
		return "-", nil
	}
	if !printableASCII.MatchString(s) {
		return "", fmt.Errorf("字段 %q 必须为 1-48 位可打印非空格 ASCII", s)
	}
	return s, nil
}

// Build 只负责构造 Syslog 数据；传输分帧必须在 transport 层处理。
func Build(format Format, f Fields) ([]byte, error) {
	if format != Raw {
		if err := f.validate(); err != nil {
			return nil, err
		}
	}
	if format == Raw {
		return []byte(f.Message), nil
	}
	if f.Timestamp.IsZero() {
		f.Timestamp = time.Now()
	}
	pri := int(f.Facility)*8 + int(f.Severity)
	switch format {
	case RFC3164:
		host := f.Hostname
		if host == "" {
			host = "localhost"
		}
		if strings.ContainsAny(host, " \t\r\n") {
			return nil, errors.New("hostname 不能含空白字符")
		}
		app := f.AppName
		if app == "" {
			app = "syslog-tester"
		}
		if strings.ContainsAny(app, ":\r\n") {
			return nil, errors.New("app-name 不合法")
		}
		result := []byte(fmt.Sprintf("<%d>%s %s %s: %s", pri, f.Timestamp.Format("Jan _2 15:04:05"), host, app, f.Message))
		if len(result) > 1024 {
			return nil, errors.New("RFC3164 消息超过传统 1024 字节限制")
		}
		return result, nil
	case RFC5424:
		names := []string{f.Hostname, f.AppName, f.ProcID, f.MsgID}
		for i := range names {
			v, err := field(names[i])
			if err != nil {
				return nil, err
			}
			names[i] = v
		}
		// 使用 NILVALUE 结构化数据，消息体保留用户输入（包括 Unicode）。
		return []byte(fmt.Sprintf("<%d>1 %s %s %s %s %s - %s", pri, f.Timestamp.Format(time.RFC3339Nano), names[0], names[1], names[2], names[3], f.Message)), nil
	default:
		return nil, fmt.Errorf("未知 Syslog 格式 %q", format)
	}
}

var priPrefix = regexp.MustCompile(`^<([0-9]{1,3})>`)

func Detect(payload []byte) Format {
	match := priPrefix.FindSubmatch(payload)
	if match == nil {
		return Raw
	}
	n, e := strconv.Atoi(string(match[1]))
	if e != nil || n > 191 {
		return Raw
	}
	tail := payload[len(match[0]):]
	if strings.HasPrefix(string(tail), "1 ") {
		return RFC5424
	}
	if len(tail) >= 16 && tail[3] == ' ' && tail[6] == ' ' && tail[9] == ':' && tail[12] == ':' && tail[15] == ' ' {
		return RFC3164
	}
	return Raw
}
