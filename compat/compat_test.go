package compat

import "testing"

func TestRange(t *testing.T) {
	cases := []struct {
		rng, ver string
		want     bool
	}{
		{">=0.5.0 <1.0.0", "0.5.0", true},
		{">=0.5.0 <1.0.0", "0.4.9", false},
		{">=0.5.0 <1.0.0", "0.9.10", true},
		{">=0.5.0 <1.0.0", "1.0.0", false},
		{">=0.5.0 <1.0.0", "v0.6.1+build5", true},
		{">=0.5.0 <1.0.0", "0.6.0-rc.1", false}, // prerelease excluded
		{">=0.6.0-rc.1 <1.0.0", "0.6.0-rc.2", true},
		{">=0.6.0-rc.1 <1.0.0", "0.6.0-beta", false},
		{">=0.6.0-rc.1 <1.0.0", "0.7.0-rc.2", false}, // other tuple
		{"<0.5.0 || >=0.7.0", "0.6.0", false},
		{"<0.5.0 || >=0.7.0", "0.8.0", true},
		{"1.2.3", "1.2.3", true},
		{">1.2.3", "1.2.3", false},
		{"<=1.2.3", "1.2.3", true},
	}
	for _, c := range cases {
		r, err := ParseRange(c.rng)
		if err != nil {
			t.Fatalf("%s: %v", c.rng, err)
		}
		v, err := ParseVersion(c.ver)
		if err != nil {
			t.Fatalf("%s: %v", c.ver, err)
		}
		if got := r.Contains(v); got != c.want {
			t.Errorf("%q contains %q = %v, want %v", c.rng, c.ver, got, c.want)
		}
	}
}

func TestPrecedence(t *testing.T) {
	order := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0"}
	for i := 0; i+1 < len(order); i++ {
		a, _ := ParseVersion(order[i])
		b, _ := ParseVersion(order[i+1])
		if a.Compare(b) != -1 || b.Compare(a) != 1 {
			t.Errorf("%s should be < %s", order[i], order[i+1])
		}
	}
}

func TestBadInput(t *testing.T) {
	for _, s := range []string{"", "1.2", "1.2.x", "01.2.3", "1.2.3-", ">=", "a"} {
		if _, err := ParseRange(s); err == nil {
			t.Errorf("range %q accepted", s)
		}
	}
	if _, err := ParseRange(">=1.0.0 ||"); err == nil {
		t.Error("trailing || accepted")
	}
}

func TestCompatibility(t *testing.T) {
	c, err := Parse([]byte(`{"core":">=0.5.0 <1.0.0","wireContract":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Check("0.5.9", ContractVersion); err != nil {
		t.Error(err)
	}
	if err := c.Check("1.0.0", ContractVersion); err == nil {
		t.Error("core out of range accepted")
	}
	if err := c.Check("0.5.9", 0); err == nil {
		t.Error("old host accepted")
	}
	c2, _ := Parse([]byte(`{"wireContract":2}`))
	if err := c2.Check("9.9.9", 1); err == nil {
		t.Error("wireContract 2 on host 1 accepted")
	}
	z, err := Parse(nil)
	if err != nil || z.Check("0.0.1", 1) != nil {
		t.Error("empty must be compatible with everything (absent = v1)")
	}
	if _, err := Parse([]byte(`{"core":"nope"}`)); err == nil {
		t.Error("bad range accepted")
	}
	if _, err := Parse([]byte(`{"wireContract":-1}`)); err == nil {
		t.Error("negative accepted")
	}
}
