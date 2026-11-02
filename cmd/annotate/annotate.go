// Package annotate implements the "annotate" CLI command: print the
// mml.yaml annotations the model is missing, prefilled where the structure
// says what they should be.
package annotate

import (
	"context"
	"fmt"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/mixedmodellint/cmd/root"
	"github.com/StevenACoffman/mixedmodellint/internal/lint"
)

// Config holds the annotate command's flags.
type Config struct {
	*root.Config
	Dir     string
	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the annotate command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("annotate").SetParent(parent.Flags)
	cfg.Flags.StringVar(&cfg.Dir, 0, "dir", ".", "directory whose model to annotate")
	cfg.Command = &ff.Command{
		Name:      "annotate",
		Usage:     "mixedmodellint annotate [--dir DIR]",
		ShortHelp: "print the mml.yaml annotations the model is missing",
		LongHelp: `Print, as an mml.yaml fragment, every annotation the declaration designs ask
for that the model does not have: each entity's grain, definition, owner and
escalation, each nullable field's null meaning and cardinality, and each
measure's class. Values the structure implies are filled in (the grain from
the primary key, with its fp=; the measure class from the field's name);
the rest are TODO, which no design accepts, so an unfilled stub is still
reported. Review the fragment and merge it into mml.yaml; annotate never
writes files.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return &root.UsageError{
			Err: fmt.Errorf("annotate: unexpected argument %q (use --dir)", args[0]),
		}
	}
	out, err := lint.Annotate(ctx, cfg.Dir)
	if err != nil {
		return fmt.Errorf("annotate: %w", err)
	}
	if _, err := cfg.Stdout.Write(out); err != nil {
		return fmt.Errorf("annotate: %w", err)
	}
	return nil
}
