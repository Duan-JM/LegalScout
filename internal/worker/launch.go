package worker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// StartDetached starts the hidden internal worker after queueing. Start is
// portable and deliberately reports an error rather than silently pretending
// the queue is running if the executable cannot be relaunched.
func StartDetached(workspaceRoot string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate legalscout executable: %w", err)
	}
	logDir := filepath.Join(workspaceRoot, "_legalscout", "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("create worker log directory: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "worker-"+time.Now().Format("20060102")+".log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open worker log: %w", err)
	}
	defer logFile.Close()
	command := exec.Command(executable, "__worker")
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	configureDetached(command)
	if err := command.Start(); err != nil {
		return fmt.Errorf("start background worker: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return fmt.Errorf("release background worker handle: %w", err)
	}
	return nil
}
