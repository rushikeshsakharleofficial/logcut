package main

import (
	"os"
	"strings"

	"github.com/rushikeshsakharleofficial/logcut/internal/cli"
)

func main() {
	args := normalizeExtraFlags(os.Args[1:])
	os.Exit(cli.Run(args))
}

func normalizeExtraFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--rate-limit" && i+1 < len(args):
			_ = os.Setenv("LOGCUT_RATE_LIMIT", args[i+1])
			i++
		case strings.HasPrefix(arg, "--rate-limit="):
			_ = os.Setenv("LOGCUT_RATE_LIMIT", strings.TrimPrefix(arg, "--rate-limit="))
		case arg == "--sleep-between-chunks" && i+1 < len(args):
			_ = os.Setenv("LOGCUT_SLEEP_BETWEEN_CHUNKS", args[i+1])
			i++
		case strings.HasPrefix(arg, "--sleep-between-chunks="):
			_ = os.Setenv("LOGCUT_SLEEP_BETWEEN_CHUNKS", strings.TrimPrefix(arg, "--sleep-between-chunks="))
		default:
			out = append(out, arg)
		}
	}
	return out
}
