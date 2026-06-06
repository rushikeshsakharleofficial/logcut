package job

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
)

func ID(source string, output string) string {
	h := sha1.Sum([]byte(source + "|" + output))
	return hex.EncodeToString(h[:])[:16]
}

func StatePath(stateDir string, source string, output string) string {
	return filepath.Join(stateDir, ID(source, output)+".state")
}

func LockPath(lockDir string, source string, output string) string {
	return filepath.Join(lockDir, "logcut-"+ID(source, output)+".lock")
}
