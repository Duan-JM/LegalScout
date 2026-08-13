// Package logging configures durable project-local structured logs.
package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/Duan-JM/LegalScout/internal/workspace"
)

func Open(project workspace.Project) (*slog.Logger, func(), error) {
	if err := os.MkdirAll(project.LogsPath(), 0o700); err != nil {
		return nil, nil, err
	}
	file, err := os.OpenFile(filepath.Join(project.LogsPath(), "worker-"+time.Now().Format("20060102")+".log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return slog.New(slog.NewJSONHandler(file, nil)), func() { _ = file.Close() }, nil
}
