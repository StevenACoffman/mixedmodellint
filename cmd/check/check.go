// Package check implements the "check" CLI command: lint a directory's Go,
// SQL and JSON Schema sources against data_model_rules.md.
package check

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/mixedmodellint/cmd/root"
	"github.com/StevenACoffman/mixedmodellint/internal/lint"
	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// exitFindings is the exit code when any finding is an error, so CI fails.
const exitFindings = 1

// Config holds the check command's flags.
type Config struct {
	*root.Config
	Dir           string
	Format        string
	Output        string
	Baseline      string
	WriteBaseline string
	Base          string
	Flags         *ff.FlagSet
	Command       *ff.Command
}

// New creates and registers the check command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("check").SetParent(parent.Flags)
	cfg.Flags.StringVar(&cfg.Dir, 0, "dir", ".", "directory to lint")
	cfg.Flags.StringVar(&cfg.Format, 0, "format", "text", "output format: text, json or sarif")
	cfg.Flags.StringVar(
		&cfg.Output,
		0,
		"output",
		"",
		"write the report to this file instead of stdout",
	)
	cfg.Flags.StringVar(&cfg.Baseline, 0, "baseline", "",
		"baseline file; findings it lists are not reported")
	cfg.Flags.StringVar(&cfg.WriteBaseline, 0, "write-baseline", "",
		"write the fingerprints of every current finding to this file")
	cfg.Flags.StringVar(&cfg.Base, 0, "base", "",
		"git ref (branch, tag or commit ID) of the base revision to compare with, "+
			"instead of mml.lock.json")
	cfg.Command = &ff.Command{
		Name:      "check",
		Usage:     "mixedmodellint check [--dir DIR] [--format text|json|sarif] [FLAGS]",
		ShortHelp: "lint Go, SQL and JSON Schema sources against the data model rules",
		LongHelp: `Lint every SQL migration (*.sql, replayed in path order), persisted Go
struct (*.go) and JSON Schema (*.schema.json, or JSON with "$schema") under
--dir, with annotations from mml.yaml at its root. Each finding names the rule
(for example 7.1) and design (7.1-B) of rule_plan.md, the measured value, and
a suggested fix.

The exit status is 1 when any finding is an error (a MUST rule), so CI fails;
warnings and notes are reported without failing. Adopt the linter on an
existing codebase with --write-baseline, then pass --baseline so only new
findings fail. A run with --write-baseline exits 0, since recording the
current findings is its purpose.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return &root.UsageError{
			Err: fmt.Errorf("check: unexpected argument %q (use --dir)", args[0]),
		}
	}
	format, err := lint.ParseFormat(cfg.Format)
	if err != nil {
		return &root.UsageError{Err: fmt.Errorf("check: --format: %s", mml.ErrorMessage(err))}
	}

	opts := lint.Options{Base: cfg.Base}
	if cfg.Baseline != "" {
		data, err := os.ReadFile(cfg.Baseline)
		if err != nil {
			return fmt.Errorf("check: read baseline: %w", err)
		}
		opts.Baseline = bytes.NewReader(data)
	}
	report, err := lint.Run(ctx, cfg.Dir, opts)
	if err != nil {
		return fmt.Errorf("check: %w", err)
	}

	if cfg.WriteBaseline != "" {
		if err := root.WriteFile(cfg.WriteBaseline, func(w io.Writer) error {
			if err := mml.WriteBaseline(w, report); err != nil {
				return fmt.Errorf("encode: %w", err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("check: write baseline: %w", err)
		}
	}
	if err := cfg.render(report, format); err != nil {
		return fmt.Errorf("check: %w", err)
	}
	if report.HasErrors() && cfg.WriteBaseline == "" {
		// Wrapped for the trace; main finds the code with errors.As and prints
		// nothing more, since the report already said what failed.
		return fmt.Errorf("check: findings: %w", root.ExitError(exitFindings))
	}
	return nil
}

func (cfg *Config) render(report mml.Report, format lint.Format) error {
	tool := lint.Tool{
		Name: "mixedmodellint", Version: moduleVersion(),
		URI: "https://github.com/StevenACoffman/mixedmodellint",
	}
	write := func(w io.Writer) error {
		if err := lint.Render(w, report, format, tool); err != nil {
			return fmt.Errorf("render: %w", err)
		}
		return nil
	}
	if cfg.Output == "" {
		return write(cfg.Stdout)
	}
	if err := root.WriteFile(cfg.Output, write); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func moduleVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "devel"
}
