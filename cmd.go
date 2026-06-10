package main

// ─── Command Dispatch ────────────────────────────────────────────────

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// dispatch routes subcommands. The bare no-args invocation is the renderer —
// that is how Claude Code calls the binary, so it must stay untouched.
// Legacy --flag spellings are accepted as aliases for each subcommand.
func dispatch() {
	if len(os.Args) > 1 {
		switch strings.TrimLeft(os.Args[1], "-") {
		case "help", "h":
			printHelp()
			return
		case "version", "v", "V":
			runVersion()
			return
		case "configure":
			runConfigure()
			return
		case "debug":
			runRender(true)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q (try: claude-statusline --help)\n", os.Args[1])
			os.Exit(2)
		}
	}
	runRender(false)
}

// runRender is the default mode: read the JSON payload from stdin and print
// the statusline. With debug=true it prints the schema-comparison table instead.
func runRender(debug bool) {
	start := time.Now()

	input := readInput()
	p := parsePayload(input)

	if debug {
		printDebugSchema(input, p)
		return
	}

	colors := currentPalette()
	cfg := loadConfig()
	initSegments(cfg.Plugins)
	lines := buildStatusline(p, colors, cfg, terminalWidth(p))

	elapsedMS := float64(time.Since(start).Microseconds()) / 1000.0
	if len(lines) > 0 {
		fmt.Printf("%s │ %s%.1fms%s\n", lines[0], colors.Dim, elapsedMS, colors.Rst)
		for _, l := range lines[1:] {
			fmt.Println(l)
		}
	} else {
		fmt.Printf("%s%.1fms%s\n", colors.Dim, elapsedMS, colors.Rst)
	}
}
