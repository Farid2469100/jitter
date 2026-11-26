package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
)

const version = "0.2.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "render":
		return runRender(args[1:])
	case "bake":
		return runBake(args[1:])
	case "-h", "--help", "help":
		printUsage()
		return nil
	case "-V", "--version", "version":
		fmt.Printf("jitter %s\n", version)
		return nil
	default:
		return fmt.Errorf("unrecognized subcommand %q", args[0])
	}
}

func runRender(args []string) error {
	fs := pflag.NewFlagSet("render", pflag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	fontPath := fs.StringP("font", "f", "", "Path to font file (.ttf or .otf)")
	output := fs.StringP("output", "o", "output.svg", "Output file path (.svg or .png)")
	intensity := fs.Float64P("intensity", "i", 0.5, "Variation intensity (0.0 to 1.0)")
	size := fs.Uint32P("size", "s", 48, "Font size in pixels")
	seed := fs.Uint64("seed", 0, "Random seed for reproducible output (u64)")
	help := fs.BoolP("help", "h", false, "Show help")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, renderUsage())
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *help {
		fmt.Print(renderUsage())
		return nil
	}

	if fs.NArg() != 1 {
		return fmt.Errorf("expected exactly one TEXT argument")
	}
	text := fs.Arg(0)

	if text == "" {
		return fmt.Errorf("text must not be empty")
	}
	if *fontPath == "" {
		return fmt.Errorf("the --font flag is required")
	}
	if math.IsNaN(*intensity) || *intensity < 0.0 || *intensity > 1.0 {
		return fmt.Errorf("intensity must be between 0.0 and 1.0, got %v", *intensity)
	}

	var seedPtr *uint64
	if fs.Changed("seed") {
		seedPtr = seed
	}

	glyphs, unitsPerEm, err := loadGlyphs(*fontPath, text)
	if err != nil {
		return err
	}

	commands := make([][]PathCommand, len(glyphs))
	for i, glyph := range glyphs {
		commands[i] = glyph.Commands
	}

	jittered := applyJitter(commands, *intensity, float64(unitsPerEm), seedPtr)

	seedNote := ""
	if seedPtr != nil {
		seedNote = fmt.Sprintf(" (seed: %d)", *seedPtr)
	}

	ext := strings.ToLower(filepath.Ext(*output))
	switch ext {
	case "", ".svg":
		svgOutput := renderSVG(glyphs, jittered, *size, unitsPerEm)
		if err := os.WriteFile(*output, []byte(svgOutput), 0o644); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
		fmt.Printf("Rendered %q -> %s (%d bytes)%s\n", text, *output, len(svgOutput), seedNote)
	case ".png":
		pngBytes, err := renderPNG(glyphs, jittered, *size, unitsPerEm)
		if err != nil {
			return fmt.Errorf("failed to render PNG: %w", err)
		}
		if err := os.WriteFile(*output, pngBytes, 0o644); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
		fmt.Printf("Rendered %q -> %s (%d bytes)%s\n", text, *output, len(pngBytes), seedNote)
	default:
		return fmt.Errorf("unsupported output extension: %s (supported: .svg, .png)", ext)
	}

	return nil
}

func runBake(args []string) error {
	fs := pflag.NewFlagSet("bake", pflag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	output := fs.StringP("output", "o", "", "Output font file path (default: <input>-jittered.ttf)")
	alternates := fs.Uint32P("alternates", "a", 3, "Number of alternate glyphs per character")
	intensity := fs.Float64P("intensity", "i", 0.5, "Variation intensity (0.0 to 1.0)")
	help := fs.BoolP("help", "h", false, "Show help")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, bakeUsage())
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *help {
		fmt.Print(bakeUsage())
		return nil
	}

	if fs.NArg() != 1 {
		return fmt.Errorf("expected exactly one INPUT argument")
	}
	input := fs.Arg(0)

	if *alternates == 0 {
		return fmt.Errorf("alternates must be at least 1")
	}
	if math.IsNaN(*intensity) || *intensity < 0.0 || *intensity > 1.0 {
		return fmt.Errorf("intensity must be between 0.0 and 1.0, got %v", *intensity)
	}

	outputPath := *output
	if outputPath == "" {
		outputPath = defaultBakeOutputPath(input)
	}

	fmt.Printf(
		"Baking %s with %d alternates (intensity: %v) -> %s\n",
		input, *alternates, *intensity, outputPath,
	)

	if err := bakeFont(input, outputPath, *alternates, *intensity); err != nil {
		return err
	}

	fmt.Printf("Wrote %s\n", outputPath)
	return nil
}

func defaultBakeOutputPath(input string) string {
	dir := filepath.Dir(input)
	base := filepath.Base(input)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" {
		// Hidden files such as ".ttf" have no stem after trimming.
		stem = base
	}
	return filepath.Join(dir, stem+"-jittered.ttf")
}

func printUsage() {
	fmt.Print(`jitter — Add natural handwriting-like variation to digital text

Usage:
  jitter render <TEXT> --font <FONT> [--output <FILE>] [--intensity <0.0-1.0>] [--size <PX>] [--seed <N>]
  jitter bake <INPUT> [--output <FILE>] [--alternates <N>] [--intensity <0.0-1.0>]

Commands:
  render  Render text with per-character random variation
  bake    Bake variation into a font file (generates calt alternates in a TTF)

Options:
  -h, --help     Show this help
  -V, --version  Show version
`)
}

func renderUsage() string {
	return `Render text with per-character random variation

Usage:
  jitter render <TEXT> --font <FONT> [options]

Options:
  -f, --font <FILE>        Path to font file (.ttf or .otf) (required)
  -o, --output <FILE>      Output file path (.svg or .png) (default: output.svg)
  -i, --intensity <FLOAT>  Variation intensity (0.0 to 1.0) (default: 0.5)
  -s, --size <PX>          Font size in pixels (default: 48)
      --seed <N>           Random seed for reproducible output (u64)
  -h, --help               Show this help
`
}

func bakeUsage() string {
	return `Bake variation into a font file (generates calt alternates in a TTF)

Usage:
  jitter bake <INPUT> [options]

Options:
  -o, --output <FILE>      Output font file path (default: <input>-jittered.ttf)
  -a, --alternates <N>     Number of alternate glyphs per character (default: 3)
  -i, --intensity <FLOAT>  Variation intensity (0.0 to 1.0) (default: 0.5)
  -h, --help               Show this help
`
}
