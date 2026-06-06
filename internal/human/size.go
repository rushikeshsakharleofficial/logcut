package human

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	KiB int64 = 1024
	MiB int64 = 1024 * KiB
	GiB int64 = 1024 * MiB
)

func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, errors.New("empty size")
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "GB"):
		mult = GiB
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "G"):
		mult = GiB
		s = strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "MB"):
		mult = MiB
		s = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "M"):
		mult = MiB
		s = strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "KB"):
		mult = KiB
		s = strings.TrimSuffix(s, "KB")
	case strings.HasSuffix(s, "K"):
		mult = KiB
		s = strings.TrimSuffix(s, "K")
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, errors.New("size must be greater than zero")
	}
	return int64(n * float64(mult)), nil
}

func FormatBytes(n int64) string {
	if n >= GiB {
		return fmt.Sprintf("%.2fG", float64(n)/float64(GiB))
	}
	if n >= MiB {
		return fmt.Sprintf("%.2fM", float64(n)/float64(MiB))
	}
	if n >= KiB {
		return fmt.Sprintf("%.2fK", float64(n)/float64(KiB))
	}
	return fmt.Sprintf("%dB", n)
}
