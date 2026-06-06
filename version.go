package main

import (
	"fmt"
	"os"
)

var version = "1.0.9"

func init() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--version", "-version", "version":
			fmt.Println("logcut", version)
			os.Exit(0)
		}
	}
}
