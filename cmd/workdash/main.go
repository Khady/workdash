package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Khady/workdash/internal/actions"
	"github.com/Khady/workdash/internal/ui"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	fs := flag.NewFlagSet("workdash", flag.ContinueOnError)
	emit := fs.String("emit", "", "Print selected action as raw shell code")
	configPath := fs.String("config", "", "Override the default XDG config path")
	emitPath := fs.String("emit-path", "", "Write selected shell command to a file instead of stdout")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	app, err := ui.NewWorkdashApp(*configPath, cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	result, returnCode, err := app.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	shell := actions.SerializeEmittedAction(result)
	if shell != "" && *emitPath != "" {
		if err := os.WriteFile(*emitPath, []byte(shell), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	} else if shell != "" && *emit == "shell" {
		fmt.Println(shell)
	} else if result != nil && result.ToShell() != "" {
		fmt.Println("Selected action: " + result.ToShell())
	}
	return returnCode
}
