package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rushikeshsakharleofficial/logcut/internal/compact"
	"github.com/rushikeshsakharleofficial/logcut/internal/job"
	"github.com/rushikeshsakharleofficial/logcut/internal/preflight"
	"github.com/rushikeshsakharleofficial/logcut/internal/state"
	"github.com/rushikeshsakharleofficial/logcut/internal/version"
)

func Run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "status":
			return statusCmd(args[1:])
		case "clean-state":
			return cleanStateCmd(args[1:])
		case "list-state":
			return listStateCmd(args[1:])
		}
	}

	cfg := compact.DefaultConfig()
	preflightOnly := false
	fs := flag.NewFlagSet("logcut", flag.ContinueOnError)
	fs.BoolVar(&cfg.GzipOutput, "g", false, "write gzip rotated archive")
	fs.StringVar(&cfg.KeepLastRaw, "k", "", "keep latest part in active log, example: 10G")
	fs.Int64Var(&cfg.WorkingPercent, "p", 20, "use only this percent of current free space as working budget")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "print plan only")
	fs.BoolVar(&cfg.Force, "force", false, "allow risky operation")
	fs.BoolVar(&cfg.Verbose, "v", false, "verbose logs with per-step and per-chunk details")
	fs.BoolVar(&cfg.Quiet, "quiet", false, "suppress progress logs")
	fs.BoolVar(&cfg.JSON, "json", false, "emit JSON events instead of human progress logs")
	fs.BoolVar(&preflightOnly, "preflight", false, "run safety checks only, do not modify source log")
	fs.DurationVar(&cfg.ProgressInterval, "progress-interval", 5*time.Second, "progress summary interval, example: 5s, 30s, 1m")
	fs.StringVar(&cfg.StopFreeAboveRaw, "stop-free-above", "", "stop safely after current chunk once free space is above this value")
	fs.DurationVar(&cfg.MaxRuntime, "max-runtime", 0, "stop safely after current chunk once runtime is reached")
	fs.StringVar(&cfg.LogFile, "log-file", "", "also write run output to this file")
	fs.IntVar(&cfg.CompressLevel, "compress-level", cfg.CompressLevel, "gzip compression level: 1 fastest, 9 best, -1 default")
	fs.StringVar(&cfg.VerifyMode, "verify", cfg.VerifyMode, "gzip verification mode: full or none")
	fs.StringVar(&cfg.StateDir, "state-dir", cfg.StateDir, "state directory")
	fs.StringVar(&cfg.LockDir, "lock-dir", cfg.LockDir, "lock directory")
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
	if cfg.ProgressInterval < 0 {
		fmt.Fprintln(os.Stderr, "Invalid --progress-interval value")
		return 2
	}
	if cfg.Quiet && cfg.Verbose {
		fmt.Fprintln(os.Stderr, "Use either --quiet or -v, not both")
		return 2
	}
	if cfg.CompressLevel < -1 || cfg.CompressLevel > 9 || cfg.CompressLevel == 0 {
		fmt.Fprintln(os.Stderr, "Invalid --compress-level. Use -1 or 1..9")
		return 2
	}
	cfg.Source = pos[0]
	cfg.Output = pos[1]

	if preflightOnly {
		res := preflight.Run(preflight.Config{Source: cfg.Source, Output: cfg.Output, StateDir: cfg.StateDir, LockDir: cfg.LockDir, Gzip: cfg.GzipOutput, KeepRaw: cfg.KeepLastRaw})
		preflight.Print(os.Stdout, res)
		if res.Failed() {
			return 1
		}
		return 0
	}

	if err := compact.Run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		return 1
	}
	return 0
}

func statusCmd(args []string) int {
	cfg := compact.DefaultConfig()
	fs := flag.NewFlagSet("logcut status", flag.ContinueOnError)
	fs.StringVar(&cfg.StateDir, "state-dir", cfg.StateDir, "state directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pos := fs.Args()
	if len(pos) != 2 {
		fmt.Println("Usage: logcut status [--state-dir DIR] <source-log> <rotated-output>")
		return 2
	}
	src, _ := filepath.Abs(pos[0])
	out, _ := filepath.Abs(pos[1])
	path := job.StatePath(cfg.StateDir, src, out)
	s, err := state.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		return 1
	}
	fmt.Println("State file:", path)
	fmt.Println("Source:", s.Source)
	fmt.Println("Output:", s.Output)
	fmt.Println("Original size:", s.OriginalSize)
	fmt.Println("Cutoff offset:", s.CutoffOffset)
	fmt.Println("Last archived offset:", s.LastArchivedOffset)
	fmt.Println("Last punched offset:", s.LastPunchedOffset)
	fmt.Println("Gzip:", s.Gzip)
	fmt.Println("Updated:", s.UpdatedAt)
	return 0
}

func cleanStateCmd(args []string) int {
	cfg := compact.DefaultConfig()
	fs := flag.NewFlagSet("logcut clean-state", flag.ContinueOnError)
	fs.StringVar(&cfg.StateDir, "state-dir", cfg.StateDir, "state directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pos := fs.Args()
	if len(pos) != 2 {
		fmt.Println("Usage: logcut clean-state [--state-dir DIR] <source-log> <rotated-output>")
		return 2
	}
	src, _ := filepath.Abs(pos[0])
	out, _ := filepath.Abs(pos[1])
	path := job.StatePath(cfg.StateDir, src, out)
	backup := path + ".cleaned." + time.Now().Format("20060102-150405")
	if err := os.Rename(path, backup); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		return 1
	}
	fmt.Println("Archived state file:", backup)
	return 0
}

func listStateCmd(args []string) int {
	cfg := compact.DefaultConfig()
	fs := flag.NewFlagSet("logcut list-state", flag.ContinueOnError)
	fs.StringVar(&cfg.StateDir, "state-dir", cfg.StateDir, "state directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	entries, err := os.ReadDir(cfg.StateDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		return 1
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".state" {
			fmt.Println(filepath.Join(cfg.StateDir, e.Name()))
		}
	}
	return 0
}

func usage() {
	fmt.Println("logcut - emergency log compaction without app restart")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  logcut [options] <source-log> <rotated-output>")
	fmt.Println("  logcut status <source-log> <rotated-output>")
	fmt.Println("  logcut list-state")
	fmt.Println("  logcut clean-state <source-log> <rotated-output>")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  logcut --preflight -g -k 10G app.log app.rotated.log.gz")
	fmt.Println("  logcut -g -k 10G --stop-free-above 20G app.log app.rotated.log.gz")
	fmt.Println("  logcut -v --log-file /var/log/logcut-run.log -g -k 10G app.log app.rotated.log.gz")
	fmt.Println("  logcut --json -g -k 10G app.log app.rotated.log.gz")
	fmt.Println("  logcut --version")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  -g                         write gzip rotated archive")
	fmt.Println("  -k <size>                  keep latest part in active log, default: 10% of source size")
	fmt.Println("  -p <percent>               use only this % of current free space as working budget, default: 20")
	fmt.Println("  -v                         verbose logs with per-step and per-chunk details")
	fmt.Println("  --preflight                run safety checks only")
	fmt.Println("  --stop-free-above <size>   stop safely once free space is above size")
	fmt.Println("  --max-runtime <duration>   stop safely once runtime is reached")
	fmt.Println("  --log-file <path>          also write run output to file")
	fmt.Println("  --json                     emit JSON events")
	fmt.Println("  --compress-level <level>   gzip level: -1 or 1..9")
	fmt.Println("  --verify <mode>            gzip verification: full or none")
	fmt.Println("  --state-dir <dir>          state directory")
	fmt.Println("  --lock-dir <dir>           lock directory")
	fmt.Println("  --dry-run                  print plan only, do not modify files")
	fmt.Println("  --force                    allow risky operation")
	fmt.Println("  --quiet                    suppress progress logs")
	fmt.Println("  --progress-interval <dur>  progress summary interval, default: 5s")
	fmt.Println("  --version                  print logcut version")
}
