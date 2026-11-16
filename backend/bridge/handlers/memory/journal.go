package memory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"time"
)

// rollbackTimeout bounds the undo of a failed change, which runs past a
// cancelled request.
const rollbackTimeout = 30 * time.Second

// savedFile is a managed file as it was before a change: its content and
// mode, or that it did not exist.
type savedFile struct {
	name    string
	data    []byte
	mode    fs.FileMode
	existed bool
}

// saveFile reads the file name under the root so a failed change can put it
// back. Anything but a regular file is refused: the change would not
// replace it either.
func (h hostRoot) saveFile(name string) (savedFile, error) {
	info, err := os.Lstat(h.path(name))
	if errors.Is(err, fs.ErrNotExist) {
		return savedFile{name: name}, nil
	}
	if err != nil {
		return savedFile{}, err
	}
	if !info.Mode().IsRegular() {
		return savedFile{}, fmt.Errorf("%s is not a regular file", name)
	}
	data, err := os.ReadFile(h.path(name))
	if err != nil {
		return savedFile{}, err
	}
	return savedFile{name: name, data: data, mode: info.Mode().Perm(), existed: true}, nil
}

// restore puts the file back as it was, or removes it if it did not exist.
func (f savedFile) restore(sys hostSystem) error {
	if !f.existed {
		if err := sys.removeFile(f.name); err != nil {
			return fmt.Errorf("remove %s: %w", f.name, err)
		}
		return nil
	}
	if err := sys.writeFile(f.name, f.data, f.mode); err != nil {
		return fmt.Errorf("restore %s: %w", f.name, err)
	}
	return nil
}

// savedParam is a kernel parameter's value before a change.
type savedParam struct {
	name, value string
}

func savedParamFor(params []savedParam, name string) bool {
	return slices.ContainsFunc(params, func(p savedParam) bool { return p.name == name })
}
