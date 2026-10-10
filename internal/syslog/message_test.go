package syslog

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFormats(t *testing.T) {
	f := Fields{Facility: 16, Severity: 6, Timestamp: time.Date(2026, 10, 10, 12, 0, 0, 0, time.FixedZone("CST", 8*3600)), Hostname: "fw01", AppName: "firewall", Message: "action=deny 中文"}
	for _, format := range []Format{RFC3164, RFC5424, Raw} {
		b, e := Build(format, f)
		if e != nil {
			t.Fatal(e)
		}
		if got := Detect(b); format != Raw && got != format {
			t.Fatalf("%s detected %s: %q", format, got, b)
		}
		if !bytes.Contains(b, []byte(f.Message)) {
			t.Fatal("content changed")
		}
	}
	b, _ := Build(RFC5424, f)
	if !strings.HasPrefix(string(b), "<134>1 2026-10-10T12:00:00+08:00 fw01 firewall - - - ") {
		t.Fatal(string(b))
	}
}
func TestValidation(t *testing.T) {
	if _, e := Build(RFC5424, Fields{Facility: 24}); e == nil {
		t.Fatal("facility")
	}
	if _, e := Build(RFC5424, Fields{Hostname: "bad name"}); e == nil {
		t.Fatal("hostname")
	}
	if _, e := Build(RFC3164, Fields{Message: strings.Repeat("a", 1100)}); e == nil {
		t.Fatal("length")
	}
	original := "<34>1 2026-01-02T01:02:03Z h a - - - 你好\n"
	b, e := Build(Raw, Fields{Message: original})
	if e != nil || string(b) != original {
		t.Fatal("raw mutated")
	}
}
