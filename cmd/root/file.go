package root

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// WriteFile creates path, calls write, and closes it, reporting the first
// error, including one from Close, which is where a full disk shows up.
func WriteFile(path string, write func(io.Writer) error) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()
	return write(f)
}
