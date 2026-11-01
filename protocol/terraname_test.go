package protocol

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseTerraNameAcceptsThreeAndFourLabels(t *testing.T) {
	cases := map[string]TerraName{
		"desk.marui.terra":     {Label: "desk", Handle: "marui"},
		"rdk.lab.marui.terra":  {Label: "rdk", Tree: "lab", Handle: "marui"},
		"a.b.terra":            {Label: "a", Handle: "b"},
		"rdk-1.lab-2.m0.terra": {Label: "rdk-1", Tree: "lab-2", Handle: "m0"},
	}
	for ref, want := range cases {
		got, err := ParseTerraName(ref)
		if err != nil {
			t.Fatalf("ParseTerraName(%q) returned %v", ref, err)
		}
		if got != want {
			t.Errorf("ParseTerraName(%q) = %+v, want %+v", ref, got, want)
		}
	}
}

// Two labels cannot say whose node it is; five would invite a reading the index
// cannot answer. Neither is repaired into a near shape.
func TestParseTerraNameRefusesOtherDepths(t *testing.T) {
	for _, ref := range []string{"marui.terra", "a.b.c.d.terra", "terra", ".terra"} {
		if _, err := ParseTerraName(ref); !errors.Is(err, ErrTerraNameInvalid) {
			t.Errorf("ParseTerraName(%q) = %v, want ErrTerraNameInvalid", ref, err)
		}
	}
}

func TestParseTerraNameNormalisesWhatPeopleAddByAccident(t *testing.T) {
	want := TerraName{Label: "desk", Handle: "marui"}
	for _, ref := range []string{
		"desk.marui.terra",
		"Desk.Marui.TERRA",
		"desk.marui.terra.",
		"  Desk.Marui.TERRA.  ",
		"\tdesk.marui.terra\n",
	} {
		got, err := ParseTerraName(ref)
		if err != nil {
			t.Fatalf("ParseTerraName(%q) returned %v", ref, err)
		}
		if got != want {
			t.Errorf("ParseTerraName(%q) = %+v, want %+v", ref, got, want)
		}
	}
	// Exactly one trailing dot is forgiven; two is a different, empty label.
	if _, err := ParseTerraName("desk.marui.terra.."); !errors.Is(err, ErrTerraNameInvalid) {
		t.Errorf("ParseTerraName(two trailing dots) = %v, want ErrTerraNameInvalid", err)
	}
}

// strings.ToLower folds U+212A KELVIN SIGN onto "k". If normalisation used it, a
// name that renders as "desk" but was typed with a look-alike would pass.
func TestParseTerraNameLowercasesASCIIOnly(t *testing.T) {
	if _, err := ParseTerraName("desK.marui.terra"); !errors.Is(err, ErrTerraNameInvalid) {
		t.Errorf("ParseTerraName(kelvin sign) = %v, want ErrTerraNameInvalid", err)
	}
	if got := NormalizeDNSLabel(" DesK "); got != "desK" {
		t.Errorf("NormalizeDNSLabel folded a non-ASCII letter: %q", got)
	}
}

func TestParseTerraNameRefusesOtherSuffixes(t *testing.T) {
	for _, ref := range []string{"desk.marui.com", "desk.marui.terra2", "desk.marui.xterra", "desk.marui", "", "   "} {
		if _, err := ParseTerraName(ref); !errors.Is(err, ErrTerraNameInvalid) {
			t.Errorf("ParseTerraName(%q) = %v, want ErrTerraNameInvalid", ref, err)
		}
	}
}

func TestParseTerraNameRefusesBadLabels(t *testing.T) {
	for _, ref := range []string{
		"a..b.terra",
		"-desk.marui.terra",
		"desk-.marui.terra",
		"desk.-marui.terra",
		"desk.lab-.marui.terra",
		"xn--80ak6aa92e.marui.terra",
		"desk.xn--p1ai.terra",
		"my_desk.marui.terra",
		"desk.marui_kim.terra",
		"my desk.marui.terra",
		"desk/1.marui.terra",
		strings.Repeat("a", 64) + ".marui.terra",
	} {
		_, err := ParseTerraName(ref)
		if !errors.Is(err, ErrTerraNameInvalid) {
			t.Errorf("ParseTerraName(%q) = %v, want ErrTerraNameInvalid", ref, err)
		}
		// The empty label is a label failure too, so the caller can tell which
		// part to rename rather than only that the whole thing is wrong.
		if !errors.Is(err, ErrDNSLabelInvalid) {
			t.Errorf("ParseTerraName(%q) = %v, want it to wrap ErrDNSLabelInvalid", ref, err)
		}
	}
}

func TestParseTerraNameLengthBound(t *testing.T) {
	label63 := strings.Repeat("a", 63)
	longest := label63 + "." + label63 + "." + label63 + ".terra"
	if _, err := ParseTerraName(longest); err != nil {
		t.Fatalf("ParseTerraName(63-character labels) returned %v", err)
	}
	tooLong := strings.Repeat("a.", 125) + "b.terra" // 257 characters
	if len(tooLong) <= maxTerraNameLength {
		t.Fatalf("fixture is %d characters, want more than %d", len(tooLong), maxTerraNameLength)
	}
	_, err := ParseTerraName(tooLong)
	if !errors.Is(err, ErrTerraNameInvalid) {
		t.Fatalf("ParseTerraName(%d characters) = %v, want ErrTerraNameInvalid", len(tooLong), err)
	}
	if !strings.Contains(err.Error(), "longer than 253") {
		t.Errorf("error %q does not say the name is too long", err)
	}
}

func TestTerraNameZone(t *testing.T) {
	cases := map[string]string{
		"rdk.lab.marui.terra": "lab.marui.terra",
		"desk.marui.terra":    "marui.terra",
	}
	for ref, want := range cases {
		name, err := ParseTerraName(ref)
		if err != nil {
			t.Fatalf("ParseTerraName(%q) returned %v", ref, err)
		}
		if got := name.Zone(); got != want {
			t.Errorf("%q Zone() = %q, want %q", ref, got, want)
		}
	}
}

// The canonical string is what gets stored and compared, so it must parse back
// to the same value from any accepted spelling.
func TestTerraNameStringRoundTrips(t *testing.T) {
	cases := map[string]string{
		"  RDK.Lab.Marui.TERRA. ": "rdk.lab.marui.terra",
		"Desk.Marui.terra.":       "desk.marui.terra",
	}
	for ref, want := range cases {
		name, err := ParseTerraName(ref)
		if err != nil {
			t.Fatalf("ParseTerraName(%q) returned %v", ref, err)
		}
		if got := name.String(); got != want {
			t.Errorf("ParseTerraName(%q).String() = %q, want %q", ref, got, want)
		}
		again, err := ParseTerraName(name.String())
		if err != nil || again != name {
			t.Errorf("ParseTerraName(%q) = %+v, %v, want %+v", name.String(), again, err, name)
		}
	}
}

func TestTerraNameJSONOmitsEmptyTree(t *testing.T) {
	raw, err := json.Marshal(TerraName{Label: "desk", Handle: "marui"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"label":"desk","handle":"marui"}`; got != want {
		t.Errorf("json = %s, want %s", got, want)
	}
}

func TestValidateDNSLabelBoundaries(t *testing.T) {
	valid := []string{"a", "0", "a-b", "a--b", "desk1", strings.Repeat("a", 63), "x-n--a", "xn-a"}
	for _, label := range valid {
		if err := ValidateDNSLabel(label); err != nil {
			t.Errorf("ValidateDNSLabel(%q) = %v, want nil", label, err)
		}
	}
	invalid := []string{
		"",
		strings.Repeat("a", 64),
		"-a",
		"a-",
		"-",
		"xn--p1ai",
		"a_b",
		"a.b",
		"a b",
		// It validates the label as given: uppercase and surrounding space are
		// the caller's to normalise, not the validator's to forgive.
		"Desk",
		" desk",
	}
	for _, label := range invalid {
		if err := ValidateDNSLabel(label); !errors.Is(err, ErrDNSLabelInvalid) {
			t.Errorf("ValidateDNSLabel(%q) = %v, want ErrDNSLabelInvalid", label, err)
		}
	}
}

func TestNormalizeDNSLabelOnlyCleans(t *testing.T) {
	if got := NormalizeDNSLabel("  Desk-1 "); got != "desk-1" {
		t.Errorf("NormalizeDNSLabel = %q, want desk-1", got)
	}
	// Cleaning is not validating: an invalid label stays invalid.
	if got := NormalizeDNSLabel(" My_Desk "); got != "my_desk" {
		t.Errorf("NormalizeDNSLabel = %q, want my_desk", got)
	}
}
