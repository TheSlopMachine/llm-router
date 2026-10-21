package cmd

import (
	"context"
	"errors"
	"os"

	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/instance"
	"github.com/TheSlopMachine/llm-router/internal/services/admin"
)

func lockPathFor(applicationConfig *config.Config) string {
	return instance.LockPath(applicationConfig.DBPath)
}

// acquireInstance takes the one-process-per-database lock. A held lock
// returns *instance.HeldError.
func acquireInstance(applicationConfig *config.Config, mode instance.Mode) (*instance.Lock, error) {
	executable, _ := os.Executable()
	return instance.Acquire(lockPathFor(applicationConfig), instance.Info{
		Mode:       mode,
		Executable: executable,
		Address:    applicationConfig.Addr,
		Version:    versionInformation.Version,
	})
}

// openAdminService opens the databases for a short account command. With no
// server running the command owns the lock and closes the files with full
// maintenance. With a server running it opens next to it and only releases
// its handles: checkpoint and vacuum belong to the server.
func openAdminService(ctx context.Context, applicationConfig *config.Config) (*admin.GormService, func(), error) {
	lock, err := acquireInstance(applicationConfig, instance.ModeMaintenance)
	serverRunning := false
	var held *instance.HeldError
	switch {
	case err == nil:
	case errors.As(err, &held):
		serverRunning = true
	default:
		return nil, nil, err
	}
	database, err := db.OpenSQLite(ctx, applicationConfig.DBPath, applicationConfig.CachePath, applicationConfig.EventsPath)
	if err != nil {
		if lock != nil {
			_ = lock.Release()
		}
		return nil, nil, err
	}
	closeDatabase := func() {
		if serverRunning {
			_ = database.Release()
			return
		}
		_ = database.Close()
		_ = lock.Release()
	}
	return admin.NewGorm(database), closeDatabase, nil
}
