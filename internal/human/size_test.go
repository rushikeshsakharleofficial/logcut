package human

import "testing"

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"1K":  KiB,
		"10M": 10 * MiB,
		"2G":  2 * GiB,
		"1GB": GiB,
	}
	for input, want := range cases {
		got, err := ParseSize(input)
		if err != nil {
			t.Fatalf("ParseSize(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseSize(%q)=%d want %d", input, got, want)
		}
	}
}

func TestParseSizeRejectsInvalid(t *testing.T) {
	for _, input := range []string{"", "0", "bad", "-1G"} {
		if _, err := ParseSize(input); err == nil {
			t.Fatalf("ParseSize(%q) expected error", input)
		}
	}
}
