package app

import (
	"fmt"
	"io"
	"os"

	"github.com/comarch/git-byline/internal/model"
)

// Attribution colors use the same palette as the HTML report, so a line
// keeps its color whether it is read in a terminal or in a dashboard:
//
//	ai             cyan
//	human          violet
//	human-override magenta
//	untracked      grey, no color meaning, honest unknown
//
// Violet is the lighter step of the scale, because the base violet reaches
// only 2.29:1 against black. Every value below clears 4.5:1 on a black
// terminal background. See docs/DESIGN.md.
const (
	ansiReset = "\x1b[0m"
	ansiDim   = "\x1b[2m"

	trueHuman     = "\x1b[38;2;178;128;223m" // violet 200, #B280DF
	trueAI        = "\x1b[38;2;0;255;255m"   // cyan, #00FFFF
	trueOverride  = "\x1b[38;2;255;0;155m"   // magenta, #FF009B
	trueUntracked = "\x1b[38;2;166;166;166m" // grey 500, #A6A6A6

	// Basic sixteen-color approximations for terminals without 24-bit
	// color. They are the closest available step, not palette values.
	basicHuman     = "\x1b[95m"
	basicAI        = "\x1b[96m"
	basicOverride  = "\x1b[35m"
	basicUntracked = "\x1b[90m"
)

// colorMode is the resolved value of the --color flag.
type colorMode string

const (
	colorAuto   colorMode = "auto"
	colorAlways colorMode = "always"
	colorNever  colorMode = "never"
)

func parseColorMode(value string) (colorMode, error) {
	switch colorMode(value) {
	case colorAuto, colorAlways, colorNever:
		return colorMode(value), nil
	default:
		return "", fmt.Errorf("--color must be auto, always, or never, got %q", value)
	}
}

// useColor reports whether output to out should carry ANSI sequences.
// Automatic mode requires a terminal, an unset NO_COLOR, and a terminal
// type other than dumb, so redirected output and pipes stay plain text.
func useColor(mode colorMode, out io.Writer) bool {
	switch mode {
	case colorNever:
		return false
	case colorAlways:
		return true
	}
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// trueColor reports whether the terminal advertises 24-bit color, which the
// palette needs to render exactly.
func trueColor() bool {
	switch os.Getenv("COLORTERM") {
	case "truecolor", "24bit":
		return true
	default:
		return false
	}
}

// labelColor returns the ANSI prefix for one attribution class.
func labelColor(author model.Author) string {
	exact := trueColor()
	switch author {
	case model.AuthorHuman:
		if exact {
			return trueHuman
		}
		return basicHuman
	case model.AuthorHumanOverride:
		if exact {
			return trueOverride
		}
		return basicOverride
	case model.AuthorAI:
		if exact {
			return trueAI
		}
		return basicAI
	default:
		if exact {
			return trueUntracked
		}
		return basicUntracked
	}
}
