package main

import (
	"fmt"
	"os"
	"strings"
)

const defaultModulePath = "github.com/rushikeshsakharleofficial/logcut"
const defaultGoVersion = "1.22"

func main() {
	modulePath := os.Getenv("LOGCUT_MODULE")
	if strings.TrimSpace(modulePath) == "" {
		modulePath = defaultModulePath
	}

	goVersion := os.Getenv("LOGCUT_GO_VERSION")
	if strings.TrimSpace(goVersion) == "" {
		goVersion = defaultGoVersion
	}

	if _, err := os.Stat("go.mod"); err == nil {
		fmt.Println("go.mod already exists")
		return
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "failed to check go.mod: %v\n", err)
		os.Exit(1)
	}

	content := fmt.Sprintf("module %s\n\ngo %s\n", modulePath, goVersion)
	if err := os.WriteFile("go.mod", []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create go.mod: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("created go.mod with module %s\n", modulePath)
}
