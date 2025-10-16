package main

import (
	"os"
)

func main() {
	if len(os.Args) <= 2 {
		os.Exit(1)
		return
	}
	command := os.Args[1]
	targetPath := os.Args[2]
	switch command {
	case "man":
		if err := Man(targetPath); err != nil {
			os.Exit(1)
			return
		}
		os.Exit(0)
	case "md":
		if err := Md(targetPath); err != nil {
			os.Exit(1)
			return
		}
		os.Exit(0)
	case "deps":
		if err := Deps(targetPath); err != nil {
			os.Exit(1)
			return
		}
		os.Exit(0)
	default:
		os.Exit(1)
		return
	}
}
