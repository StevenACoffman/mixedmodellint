// Package lock implements the "lock" CLI command: record the model's
// structure in mml.lock.json (rule_plan.md §6, M3).
package lock

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/mixedmodellint/cmd/root"
	"github.com/StevenACoffman/mixedmodellint/internal/lint"
	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// Config holds the lock command's flags.
type Config struct {
	*root.Config
	Dir     string
	Output  string
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the lock command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("lock").SetParent(parent.Flags)
	cfg.Flags.StringVar(&cfg.Dir, 0, "dir", ".", "directory whose model to lock")
	cfg.Flags.StringVar(&cfg.Output, 0, "output", "",
		"write the lock to this file instead of "+lint.LockName+" in --dir; - for stdout")
	cfg.Command = &ff.Command{
		Name:      "lock",
		Usage:     "mixedmodellint lock [--dir DIR] [--output FILE]",
		ShortHelp: "record the model's structure in " + lint.LockName,
		LongHelp: `Write ` + lint.LockName + ` at the root of --dir: every entity's fields, types,
nullability, keys and references, sorted so the file diffs well. Commit it
with each model change. check compares the sources with it, reports any
difference as mml/lock-stale, and runs the designs that need to know what
changed (new tables, renamed keys, removed data) without git history.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return &root.UsageError{
			Err: fmt.Errorf("lock: unexpected argument %q (use --dir)", args[0]),
		}
	}
	l, err := lint.Lock(ctx, cfg.Dir)
	if err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	write := func(w io.Writer) error {
		if err := mml.WriteLock(w, l); err != nil {
			return fmt.Errorf("encode: %w", err)
		}
		return nil
	}
	switch cfg.Output {
	case "-":
		err = write(cfg.Stdout)
	case "":
		err = root.WriteFile(filepath.Join(cfg.Dir, lint.LockName), write)
	default:
		err = root.WriteFile(cfg.Output, write)
	}
	if err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	return nil
}
