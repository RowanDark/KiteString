package proute

import (
	"strings"
	"testing"
)

// --- LengthRange.Contains ---

func TestLengthRangeContains(t *testing.T) {
	cases := []struct {
		spec string
		n    int
		want bool
	}{
		{"1234", 1234, true},
		{"1234", 1233, false},
		{"1234", 1235, false},
		{"100-105", 100, true},
		{"100-105", 103, true},
		{"100-105", 105, true},
		{"100-105", 99, false},
		{"100-105", 106, false},
		{"0-0", 0, true},
		{"0-0", 1, false},
	}
	for _, tc := range cases {
		lr, err := ParseLengthRange(tc.spec)
		if err != nil {
			t.Fatalf("ParseLengthRange(%q): %v", tc.spec, err)
		}
		got := lr.Contains(tc.n)
		if got != tc.want {
			t.Errorf("LengthRange(%q).Contains(%d) = %v, want %v", tc.spec, tc.n, got, tc.want)
		}
	}
}

func TestParseLengthRangeErrors(t *testing.T) {
	bad := []string{"abc", "1-abc", "abc-1", ""}
	for _, s := range bad {
		_, err := ParseLengthRange(s)
		if err == nil {
			t.Errorf("ParseLengthRange(%q) expected error, got nil", s)
		}
	}
}

// --- Crumb.GenerateValue ---

func TestGenerateValueUUID(t *testing.T) {
	c := Crumb{Type: CrumbUUID}
	v := c.GenerateValue()
	parts := strings.Split(v, "-")
	if len(parts) != 5 {
		t.Errorf("UUID should have 5 parts, got %q", v)
	}
}

func TestGenerateValueString(t *testing.T) {
	c := Crumb{Type: CrumbString}
	if v := c.GenerateValue(); v == "" {
		t.Error("CrumbString should return non-empty")
	}
}

func TestGenerateValueInt(t *testing.T) {
	c := Crumb{Type: CrumbInt}
	v := c.GenerateValue()
	if v == "" {
		t.Error("CrumbInt returned empty")
	}
	for _, ch := range v {
		if ch < '0' || ch > '9' {
			t.Errorf("CrumbInt returned non-numeric %q", v)
			break
		}
	}
}

func TestGenerateValueFloat(t *testing.T) {
	c := Crumb{Type: CrumbFloat}
	v := c.GenerateValue()
	if !strings.Contains(v, ".") {
		t.Errorf("CrumbFloat should contain decimal point, got %q", v)
	}
}

func TestGenerateValueBool(t *testing.T) {
	c := Crumb{Type: CrumbBool}
	v := c.GenerateValue()
	if v != "true" && v != "false" {
		t.Errorf("CrumbBool should be true or false, got %q", v)
	}
}

func TestGenerateValueEmail(t *testing.T) {
	c := Crumb{Type: CrumbEmail}
	v := c.GenerateValue()
	if !strings.Contains(v, "@") {
		t.Errorf("CrumbEmail should contain @, got %q", v)
	}
}

func TestGenerateValueRandomString(t *testing.T) {
	c := Crumb{Type: CrumbRandomString}
	v := c.GenerateValue()
	if len(v) == 0 {
		t.Error("CrumbRandomString returned empty")
	}
}

func TestGenerateValueExample(t *testing.T) {
	c := Crumb{Type: CrumbInt, Example: "42"}
	if v := c.GenerateValue(); v != "42" {
		t.Errorf("Expected example value 42, got %q", v)
	}
}
