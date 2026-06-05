package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const goMod = `module github.com/rushikeshsakharleofficial/logcut

go 1.22
`

const nfpmConfig = `name: logcut
arch: amd64
platform: linux
version: 1.0.0
release: 1
section: admin
priority: optional
maintainer: Rushikesh Sakharle <rishiananya123@gmail.com>
description: |
  Emergency log compaction and rotation tool for Linux servers.
  logcut streams old data from huge active log files, optionally gzip-compresses it,
  and frees matching source blocks using Linux hole punching without restarting the app.
license: MIT
homepage: https://github.com/rushikeshsakharleofficial/logcut
contents:
  - src: ./build/logcut
    dst: /usr/local/bin/logcut
    file_info:
      mode: 0755
  - dst: /etc/logcut
    type: dir
    file_info:
      mode: 0755
  - dst: /var/lib/logcut
    type: dir
    file_info:
      mode: 0755
  - dst: /var/log
    type: dir
    file_info:
      mode: 0755
  - dst: /var/lock
    type: dir
    file_info:
      mode: 0755
`

func ensureFile(path string, content string) error {
	if _, err := os.Stat(path); err == nil {
		fmt.Println("exists:", path)
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && filepath.Dir(path) != "." {
		return err
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Println("created:", path)
	return nil
}

func main() {
	if err := ensureFile("go.mod", goMod); err != nil {
		fmt.Fprintln(os.Stderr, "failed to create go.mod:", err)
		os.Exit(1)
	}
	if err := ensureFile("nfpm.yaml", nfpmConfig); err != nil {
		fmt.Fprintln(os.Stderr, "failed to create nfpm.yaml:", err)
		os.Exit(1)
	}
	fmt.Println("bootstrap complete")
}
