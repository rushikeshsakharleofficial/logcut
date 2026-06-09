package human

import "testing"

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"1K":    KiB,
		"1.5K":  1536,
		"10M":   10 * MiB,
		"2G":    2 * GiB,
		"1GB":   GiB,
		" 2 mb": 2 * MiB,
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
	for _, input := range []string{"", "0", "bad", "-1G", "NaN", "Inf", "0.0001K", "9223372036854775808G"} {
		if _, err := ParseSize(input); err == nil {
			t.Fatalf("ParseSize(%q) expected error", input)
		}
	}
}
