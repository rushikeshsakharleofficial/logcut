package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/rushikeshsakharleofficial/logcut/internal/compact"
	"github.com/rushikeshsakharleofficial/logcut/internal/version"
)

func Run(args []string) int {
	cfg := compact.DefaultConfig()
	fs := flag.NewFlagSet("logcut", flag.ContinueOnError)
	fs.BoolVar(&cfg.GzipOutput, "g", false, "write gzip rotated archive")
	fs.StringVar(&cfg.KeepLastRaw, "k", "", "keep latest part in active log, example: 10G")
	fs.Int64Var(&cfg.WorkingPercent, "p", 20, "use only this percent of current free space as working budget")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "print plan only")
	fs.BoolVar(&cfg.Force, "force", false, "allow risky operation")
	fs.Usage = usage

	for _, arg := range args {
		if arg == "--version" || arg == "-version" || arg == "version" {
			fmt.Println("logcut", version.String())
			return 0
		}
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	pos := fs.Args()
	if len(pos) != 2 {
		usage()
		return 2
	}
	if cfg.WorkingPercent <= 0 || cfg.WorkingPercent > 80 {
		fmt.Fprintln(os.Stderr, "Invalid -p value. Use 1 to 80. Recommended: 20")
		return 2
	}
	cfg.Source = pos[0]
	cfg.Output = pos[1]
	if err := compact.Run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Println("logcut - emergency log compaction without app restart")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  logcut [options] <source-log> <rotated-output>")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  logcut app.log app.rotated.log")
	fmt.Println("  logcut -g app.log app.rotated.log.gz")
	fmt.Println("  logcut -g -k 10G app.log app.rotated.log.gz")
	fmt.Println("  logcut --dry-run -g -k 10G app.log app.rotated.log.gz")
	fmt.Println("  logcut --version")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  -g              write gzip rotated archive")
	fmt.Println("  -k <size>       keep latest part in active log, default: 10% of source size")
	fmt.Println("  -p <percent>    use only this % of current free space as working budget, default: 20")
	fmt.Println("  --dry-run       print plan only, do not modify files")
	fmt.Println("  --force         allow risky operation")
	fmt.Println("  --version       print logcut version")
}
