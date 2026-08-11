package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type Lock struct {
	file *os.File
}

func Acquire(dataDir string) (*Lock, error) {
	path := filepath.Join(dataDir, "daemon.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		holderPID := readPID(f)
		f.Close()
		if holderPID > 0 {
			return nil, fmt.Errorf("another burnrate daemon (pid %d) is already running on this data dir", holderPID)
		}
		return nil, fmt.Errorf("another burnrate daemon is already running on this data dir (lock held)")
	}

	f.Truncate(0)
	f.Seek(0, 0)
	fmt.Fprintf(f, "%d\n", os.Getpid())
	f.Sync()

	return &Lock{file: f}, nil
}

func (l *Lock) Release() error {
	if l.file == nil {
		return nil
	}
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	name := l.file.Name()
	l.file.Close()
	os.Remove(name)
	return nil
}

func readPID(f *os.File) int {
	f.Seek(0, 0)
	buf := make([]byte, 32)
	n, err := f.Read(buf)
	if err != nil || n == 0 {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil {
		return 0
	}
	return pid
}
