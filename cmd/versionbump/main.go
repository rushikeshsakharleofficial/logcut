package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const versionFile = "VERSION.txt"

func main() {
	version, err := readVersion()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to read version:", err)
		os.Exit(1)
	}

	next, err := bumpPatch(version)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to bump version:", err)
		os.Exit(1)
	}

	if err := os.WriteFile(versionFile, []byte(next+"\n"), 0644); err != nil {
		fmt.Fprintln(os.Stderr, "failed to write version file:", err)
		os.Exit(1)
	}

	mustUpdateFile("nfpm.yaml", regexp.MustCompile(`(?m)^version: .*`), "version: "+next)
	mustUpdateFile("man/logcut.8", regexp.MustCompile(`logcut [0-9]+\.[0-9]+\.[0-9]+`), "logcut "+next)
	mustUpdateFile("cmd/devtool/main.go", regexp.MustCompile(`getenv\("VERSION", "[0-9]+\.[0-9]+\.[0-9]+"\)`), `getenv("VERSION", "`+next+`")`)
	mustUpdateFile("version.go", regexp.MustCompile(`var version = "[0-9]+\.[0-9]+\.[0-9]+"`), `var version = "`+next+`"`)

	fmt.Println(next)
}

func readVersion() (string, error) {
	b, err := os.ReadFile(versionFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func bumpPatch(v string) (string, error) {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("expected semantic version MAJOR.MINOR.PATCH, got %q", v)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", err
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", err
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", err
	}
	patch++
	return fmt.Sprintf("%d.%d.%d", major, minor, patch), nil
}

func mustUpdateFile(path string, re *regexp.Regexp, replacement string) {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to read", path+":", err)
		os.Exit(1)
	}
	old := string(b)
	updated := re.ReplaceAllString(old, replacement)
	if updated == old {
		fmt.Fprintln(os.Stderr, "no version pattern matched in", path)
		os.Exit(1)
	}
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		fmt.Fprintln(os.Stderr, "failed to update", path+":", err)
		os.Exit(1)
	}
}
