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
	targetDir := os.Args[2]
	switch command {
	case "man":
		if err := Man(targetDir); err != nil {
			os.Exit(1)
			return
		}
		os.Exit(0)
	case "md":
		if err := Md(targetDir); err != nil {
			os.Exit(1)
			return
		}
		os.Exit(0)
	default:
		os.Exit(1)
		return
	}
}
