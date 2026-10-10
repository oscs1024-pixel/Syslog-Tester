package main

import (
	"testing"
	"time"
)

func TestTargetFormDefaults(t *testing.T) {
	f := blankTargetForm()
	if f.Protocol != "udp" || f.Port != "514" || f.ConnectTimeoutMS != "5000" || f.WriteTimeoutMS != "5000" {
		t.Fatalf("bad defaults: %+v", f)
	}
}
func TestParseTimeoutMS(t *testing.T) {
	cases := []struct {
		in    string
		want  time.Duration
		valid bool
	}{{"1", time.Millisecond, true}, {"5000", 5 * time.Second, true}, {"60000", time.Minute, true}, {"0", 0, false}, {"60001", 0, false}, {"invalid", 0, false}}
	for _, tc := range cases {
		got, err := parseTimeoutMS(tc.in)
		if (err == nil) != tc.valid || (tc.valid && got != tc.want) {
			t.Errorf("parse %q: %s, %v", tc.in, got, err)
		}
	}
}
