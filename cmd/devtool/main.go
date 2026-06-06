package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	appName       = "logcut"
	defaultModule = "github.com/rushikeshsakharleofficial/logcut"
	defaultGo     = "1.22"
)

type config struct {
	Version    string
	Prefix     string
	BinDir     string
	SysconfDir string
	VarLibDir  string
	LogDir     string
	LockDir    string
	DestDir    string
	Goos       string
	Goarch     string
	CgoEnabled string
	NFPMModule string
	BuildDir   string
	DistDir    string
	Source     string
}

func getenv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func loadConfig() config {
	prefix := getenv("PREFIX", "/usr/local")
	return config{
		Version:    getenv("VERSION", "1.0.24"),
		Prefix:     prefix,
		BinDir:     getenv("BINDIR", filepath.Join(prefix, "bin")),
		SysconfDir: getenv("SYSCONFDIR", "/etc/logcut"),
		VarLibDir:  getenv("VARLIBDIR", "/var/lib/logcut"),
		LogDir:     getenv("LOGDIR", "/var/log"),
		LockDir:    getenv("LOCKDIR", "/var/lock"),
		DestDir:    getenv("DESTDIR", ""),
		Goos:       getenv("GOOS", "linux"),
		Goarch:     getenv("GOARCH", runtime.GOARCH),
		CgoEnabled: getenv("CGO_ENABLED", "0"),
		NFPMModule: getenv("NFPM_MODULE", "github.com/goreleaser/nfpm/v2/cmd/nfpm@latest"),
		BuildDir:   getenv("BUILD_DIR", "build"),
		DistDir:    getenv("DIST_DIR", "dist"),
		Source:     getenv("SRC", "."),
	}
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cfg := loadConfig()
	var err error

	switch os.Args[1] {
	case "modulecheck":
		err = ensureModule()
	case "build":
		err = build(cfg)
	case "install":
		err = install(cfg)
	case "uninstall":
		err = uninstall(cfg)
	case "clean":
		err = clean(cfg)
	case "deb":
		err = packageWithNFPM(cfg, "deb")
	case "rpm":
		err = packageWithNFPM(cfg, "rpm")
	case "tar":
		err = sourceTarball(cfg)
	case "checksums":
		err = checksums(cfg)
	case "dist":
		if err = sourceTarball(cfg); err == nil {
			if err = packageWithNFPM(cfg, "deb"); err == nil {
				err = packageWithNFPM(cfg, "rpm")
			}
		}
	case "help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("logcut devtool")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  go run ./cmd/devtool <target>")
	fmt.Println("")
	fmt.Println("Targets:")
	fmt.Println("  modulecheck   Create go.mod if missing")
	fmt.Println("  build         Build logcut")
	fmt.Println("  install       Install binary, directories, docs, and man page")
	fmt.Println("  uninstall     Remove installed binary and man page only")
	fmt.Println("  clean         Remove build/dist directories")
	fmt.Println("  deb           Build .deb using the nFPM Go module")
	fmt.Println("  rpm           Build .rpm using the nFPM Go module")
	fmt.Println("  tar           Build source tarball using Go archive APIs")
	fmt.Println("  checksums     Generate SHA256SUMS using Go crypto APIs")
	fmt.Println("  dist          Build tar, deb, and rpm")
	fmt.Println("")
	fmt.Println("Configuration environment variables:")
	fmt.Println("  PREFIX, BINDIR, SYSCONFDIR, VARLIBDIR, LOGDIR, LOCKDIR, DESTDIR")
}

func ensureModule() error {
	if _, err := os.Stat("go.mod"); err == nil {
		fmt.Println("go.mod already exists")
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	modulePath := getenv("LOGCUT_MODULE", defaultModule)
	goVersion := getenv("LOGCUT_GO_VERSION", defaultGo)
	content := fmt.Sprintf("module %s\n\ngo %s\n", modulePath, goVersion)
	if err := os.WriteFile("go.mod", []byte(content), 0644); err != nil {
		return err
	}
	fmt.Println("created go.mod")
	return nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	return cmd.Run()
}

func build(cfg config) error {
	if err := ensureModule(); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.BuildDir, 0755); err != nil {
		return err
	}
	bin := filepath.Join(cfg.BuildDir, appName)
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", fmt.Sprintf("-s -w -X main.version=%s", cfg.Version), "-o", bin, cfg.Source)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(), "CGO_ENABLED="+cfg.CgoEnabled, "GOOS="+cfg.Goos, "GOARCH="+cfg.Goarch)
	if err := cmd.Run(); err != nil {
		return err
	}
	fmt.Println("Built", bin)
	return nil
}

func install(cfg config) error {
	if err := build(cfg); err != nil {
		return err
	}
	binDir := withDest(cfg.DestDir, cfg.BinDir)
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(cfg.BuildDir, appName), filepath.Join(binDir, appName), 0755); err != nil {
		return err
	}
	dirs := []string{
		cfg.SysconfDir,
		cfg.VarLibDir,
		cfg.LogDir,
		cfg.LockDir,
		"/usr/share/man/man8",
		"/usr/share/doc/logcut",
		"/usr/share/doc/logcut/examples",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(withDest(cfg.DestDir, dir), 0755); err != nil {
			return err
		}
	}
	optionalCopy("man/logcut.8", withDest(cfg.DestDir, "/usr/share/man/man8/logcut.8"), 0644)
	optionalCopy("README.md", withDest(cfg.DestDir, "/usr/share/doc/logcut/README.md"), 0644)
	optionalCopy("MANUAL.md", withDest(cfg.DestDir, "/usr/share/doc/logcut/MANUAL.md"), 0644)
	optionalCopy("INSTALL.md", withDest(cfg.DestDir, "/usr/share/doc/logcut/INSTALL.md"), 0644)
	optionalCopy("LICENSE", withDest(cfg.DestDir, "/usr/share/doc/logcut/LICENSE"), 0644)
	optionalCopy("docs/architecture.md", withDest(cfg.DestDir, "/usr/share/doc/logcut/architecture.md"), 0644)
	optionalCopy("examples/emergency.md", withDest(cfg.DestDir, "/usr/share/doc/logcut/examples/emergency.md"), 0644)
	fmt.Println("Installed", filepath.Join(binDir, appName))
	fmt.Println("Man page installed to", withDest(cfg.DestDir, "/usr/share/man/man8/logcut.8"))
	return nil
}

func uninstall(cfg config) error {
	paths := []string{
		filepath.Join(withDest(cfg.DestDir, cfg.BinDir), appName),
		withDest(cfg.DestDir, "/usr/share/man/man8/logcut.8"),
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Println("Removed", path)
	}
	fmt.Println("Keeping", cfg.SysconfDir+",", cfg.VarLibDir+", and logs for safety.")
	return nil
}

func clean(cfg config) error {
	for _, dir := range []string{cfg.BuildDir, cfg.DistDir} {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	fmt.Println("Cleaned build artifacts")
	return nil
}

func packageWithNFPM(cfg config, packager string) error {
	if err := build(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DistDir, 0755); err != nil {
		return err
	}
	target := filepath.Join(cfg.DistDir, fmt.Sprintf("%s_%s_amd64.%s", appName, cfg.Version, packager))
	if packager == "rpm" {
		target = filepath.Join(cfg.DistDir, fmt.Sprintf("%s-%s-1.x86_64.rpm", appName, cfg.Version))
	}
	if err := run("go", "run", cfg.NFPMModule, "package", "--packager", packager, "--config", "nfpm.yaml", "--target", target); err != nil {
		return err
	}
	if st, err := os.Stat(target); err != nil || st.Size() == 0 {
		if err != nil {
			return fmt.Errorf("package was not created: %s: %w", target, err)
		}
		return fmt.Errorf("package was created but is empty: %s", target)
	}
	fmt.Println("Created", target)
	return nil
}

func sourceTarball(cfg config) error {
	if err := clean(cfg); err != nil {
		return err
	}
	base := filepath.Join(cfg.DistDir, fmt.Sprintf("%s-%s", appName, cfg.Version))
	if err := os.MkdirAll(base, 0755); err != nil {
		return err
	}
	files := []string{"logcut.go", "version.go", "go.mod", "Makefile", "configure", "nfpm.yaml", "README.md", "INSTALL.md", "MANUAL.md", "LICENSE", "man/logcut.8", "docs/architecture.md", "examples/emergency.md", ".github/workflows/build-packages.yml", ".github/workflows/package-commit.yml"}
	for _, f := range files {
		if _, err := os.Stat(f); err == nil {
			if err := copyFile(f, filepath.Join(base, f), 0644); err != nil {
				return err
			}
		}
	}
	outPath := filepath.Join(cfg.DistDir, fmt.Sprintf("%s-%s.tar.gz", appName, cfg.Version))
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	return filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(cfg.DistDir, path)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
}

func checksums(cfg config) error {
	if err := os.MkdirAll(cfg.DistDir, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(cfg.DistDir)
	if err != nil {
		return err
	}
	out, err := os.Create(filepath.Join(cfg.DistDir, "SHA256SUMS"))
	if err != nil {
		return err
	}
	defer out.Close()
	for _, e := range entries {
		if e.IsDir() || e.Name() == "SHA256SUMS" {
			continue
		}
		path := filepath.Join(cfg.DistDir, e.Name())
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return err
		}
		f.Close()
		fmt.Fprintf(out, "%x  %s\n", h.Sum(nil), e.Name())
	}
	fmt.Println("Created", filepath.Join(cfg.DistDir, "SHA256SUMS"))
	return nil
}

func optionalCopy(src, dst string, mode os.FileMode) {
	if _, err := os.Stat(src); err == nil {
		if err := copyFile(src, dst, mode); err != nil {
			fmt.Fprintln(os.Stderr, "warning:", err)
		}
	}
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Chmod(mode); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func withDest(destDir, path string) string {
	if destDir == "" {
		return path
	}
	return filepath.Join(destDir, strings.TrimPrefix(path, string(os.PathSeparator)))
}
