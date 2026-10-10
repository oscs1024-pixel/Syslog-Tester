package source

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTXTLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.TXT")
	_ = os.WriteFile(p, []byte("\xef\xbb\xbfalpha\r\n\n中文\nlast"), 0600)
	f, e := TXT(p, Lines)()
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	var got []string
	for {
		b, e := f.Next(context.Background())
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		got = append(got, string(b))
	}
	if !reflect.DeepEqual(got, []string{"alpha", "中文", "last"}) {
		t.Fatal(got)
	}
}
func TestTXTWholeAndBounds(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	_ = os.WriteFile(p, []byte("line1\nline2\n"), 0600)
	f, e := TXT(p, Whole)()
	if e != nil {
		t.Fatal(e)
	}
	b, e := f.Next(context.Background())
	if e != nil || string(b) != "line1\nline2\n" {
		t.Fatalf("%q %v", b, e)
	}
	_ = f.Close()
	_ = os.WriteFile(p, []byte(strings.Repeat("x", MaxRecordBytes+1)), 0600)
	f, _ = TXT(p, Lines)()
	_, e = f.Next(context.Background())
	_ = f.Close()
	if e == nil {
		t.Fatal("unbounded")
	}
	if _, e := TXT("a.csv", Lines)(); e == nil {
		t.Fatal("csv accepted")
	}
}
