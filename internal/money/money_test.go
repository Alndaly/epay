package money

import "testing"

func TestParse(t *testing.T) {
	ok := map[string]Cents{"10": 1000, "0.5": 50, "12.34": 1234, " 1.00 ": 100, "0.01": 1}
	for in, want := range ok {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "-1", "1.234", "1.", ".5", "1e3", "abc", "1,00", "9999999999999"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) should fail", in)
		}
	}
}

func TestStringAndConvert(t *testing.T) {
	if s := Cents(1234).String(); s != "12.34" {
		t.Errorf("got %s", s)
	}
	if s := Cents(5).String(); s != "0.05" {
		t.Errorf("got %s", s)
	}
	// 100 元 * 0.1389 = 13.89 美元
	if c := Cents(10000).Convert(0.1389); c != 1389 {
		t.Errorf("got %d", c)
	}
}
