//go:build unix

package lock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

type Lock struct{ f *os.File }

var ErrHeld = errors.New("outro agente Pinglume já está usando este diretório de dados")

func Acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrHeld
		}
		return nil, err
	}
	f.Truncate(0)
	fmt.Fprintf(f, "%d\n", os.Getpid())
	return &Lock{f: f}, nil
}

func (l *Lock) Release() error { return l.f.Close() }
