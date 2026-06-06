package disk

import (
	"os"
	"path/filepath"
	"syscall"
)

const (
	FALLOC_FL_KEEP_SIZE  = 0x01
	FALLOC_FL_PUNCH_HOLE = 0x02
)

type FileStats struct {
	Size       int64
	BlocksUsed int64
	Inode      uint64
	Device     uint64
}

func FileInfo(path string) (FileStats, error) {
	st, err := os.Stat(path)
	if err != nil {
		return FileStats{}, err
	}
	out := FileStats{Size: st.Size(), BlocksUsed: st.Size()}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		out.BlocksUsed = int64(sys.Blocks) * 512
		out.Inode = sys.Ino
		out.Device = uint64(sys.Dev)
	}
	return out, nil
}

func FreeBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	target := path
	if _, err := os.Stat(target); err != nil {
		target = filepath.Dir(path)
	}
	if err := syscall.Statfs(target, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func PunchHole(f *os.File, offset, length int64) error {
	if length <= 0 {
		return nil
	}
	_, _, errno := syscall.Syscall6(
		syscall.SYS_FALLOCATE,
		uintptr(f.Fd()),
		uintptr(FALLOC_FL_KEEP_SIZE|FALLOC_FL_PUNCH_HOLE),
		uintptr(offset),
		uintptr(length),
		0,
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func TestPunchHole(dir string) error {
	p := filepath.Join(dir, ".logcut-punch-test")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(p)
	defer f.Close()
	if err := f.Truncate(2 * 1024 * 1024); err != nil {
		return err
	}
	return PunchHole(f, 0, 1024*1024)
}
