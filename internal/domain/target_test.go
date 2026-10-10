package domain

import "testing"

func TestNormalize(t *testing.T) {
	in := Target{Name: "collect", Host: "2001:db8::1", Port: 514, Protocol: TCP}
	out, e := in.Normalize()
	if e != nil {
		t.Fatal(e)
	}
	if out.Framing != OctetCounting || out.Address() != "[2001:db8::1]:514" {
		t.Fatalf("%+v", out)
	}
	for _, v := range []Target{{Name: "", Host: "a", Port: 1, Protocol: UDP}, {Name: "a", Host: "a\nb", Port: 1, Protocol: UDP}, {Name: "a", Host: "a", Port: 0, Protocol: UDP}, {Name: "a", Host: "a", Port: 1, Protocol: "https"}} {
		if _, e := v.Normalize(); e == nil {
			t.Fatalf("accepted %+v", v)
		}
	}
}
