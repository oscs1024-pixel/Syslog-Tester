package source

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const MaxRecordBytes = 1024 * 1024

type Reader interface {
	Next(context.Context) ([]byte, error)
	Close() error
}
type Factory func() (Reader, error)

type one struct {
	payload []byte
	used    bool
}

func Single(b []byte) Factory {
	cp := bytes.Clone(b)
	return func() (Reader, error) { return &one{payload: cp}, nil }
}
func (s *one) Next(ctx context.Context) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if s.used {
		return nil, io.EOF
	}
	s.used = true
	if len(s.payload) == 0 {
		return nil, errors.New("日志内容为空")
	}
	return bytes.Clone(s.payload), nil
}
func (s *one) Close() error { return nil }

type Mode string

const (
	Lines Mode = "lines"
	Whole Mode = "whole"
)

type txt struct {
	file   *os.File
	reader *bufio.Reader
	mode   Mode
	first  bool
	used   bool
}

func TXT(path string, mode Mode) Factory {
	return func() (Reader, error) {
		if !strings.EqualFold(strings.TrimPrefix(path[strings.LastIndex(path, ".")+1:], "."), "txt") {
			return nil, errors.New("仅支持 TXT 文件")
		}
		if mode != Lines && mode != Whole {
			return nil, errors.New("未知 TXT 读取模式")
		}
		f, e := os.Open(path)
		if e != nil {
			return nil, e
		}
		return &txt{file: f, reader: bufio.NewReaderSize(f, 64*1024), mode: mode, first: true}, nil
	}
}
func (s *txt) Next(ctx context.Context) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if s.mode == Whole {
		if s.used {
			return nil, io.EOF
		}
		s.used = true
		data, e := io.ReadAll(io.LimitReader(s.reader, MaxRecordBytes+1))
		if e != nil {
			return nil, e
		}
		if len(data) > MaxRecordBytes {
			return nil, errors.New("整文件日志超过 1 MiB 上限")
		}
		if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
			data = data[3:]
		}
		if len(data) == 0 {
			return nil, errors.New("TXT 为空")
		}
		return data, nil
	}
	for {
		var line []byte
		for {
			if e := ctx.Err(); e != nil {
				return nil, e
			}
			frag, e := s.reader.ReadSlice('\n')
			line = append(line, frag...)
			if len(line) > MaxRecordBytes {
				return nil, fmt.Errorf("单行日志超过 %d 字节", MaxRecordBytes)
			}
			if e == bufio.ErrBufferFull {
				continue
			}
			if e != nil && e != io.EOF {
				return nil, e
			}
			if e == io.EOF && len(line) == 0 {
				return nil, io.EOF
			}
			break
		}
		line = bytes.TrimSuffix(line, []byte("\n"))
		line = bytes.TrimSuffix(line, []byte("\r"))
		if s.first {
			s.first = false
			line = bytes.TrimPrefix(line, []byte{0xef, 0xbb, 0xbf})
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		return line, nil
	}
}
func (s *txt) Close() error { return s.file.Close() }
