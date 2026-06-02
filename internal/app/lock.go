package app

import (
	"context"

	"github.com/restic/restic/internal/global"
	"github.com/restic/restic/internal/profile"
	"github.com/restic/restic/internal/repository"
	"github.com/restic/restic/internal/ui/progress"
)

type openLockConfig struct {
	dryRun    bool
	skipLock  bool
	exclusive bool
}

func internalOpenWithLocked(ctx context.Context, gopts global.Options, cfg openLockConfig, printer progress.Printer) (context.Context, *repository.Repository, func(), error) {
	repo, err := global.OpenRepository(ctx, gopts, printer)
	if err != nil {
		return nil, nil, nil, err
	}
	profile.Mark("lock: after OpenRepository")

	if cfg.dryRun {
		repo.SetDryRun()
	}

	unlock := func() {}
	if !cfg.dryRun && !cfg.skipLock {
		var lock *repository.Unlocker

		lock, ctx, err = repository.Lock(ctx, repo, cfg.exclusive, gopts.RetryLock, func(msg string) {
			if !gopts.JSON {
				printer.P("%s", msg)
			}
		}, printer.E)
		if err != nil {
			return nil, nil, nil, err
		}
		profile.Mark("lock: repository.Lock")

		unlock = lock.Unlock
	} else if cfg.skipLock && !cfg.dryRun {
		profile.Mark("lock: skipped (fast/no-lock)")
	}

	return ctx, repo, unlock, nil
}

func skipLock(gopts global.Options) bool {
	return gopts.Fast || gopts.NoLock
}

func openWithReadLock(ctx context.Context, gopts global.Options, noLock bool, printer progress.Printer) (context.Context, *repository.Repository, func(), error) {
	return internalOpenWithLocked(ctx, gopts, openLockConfig{
		dryRun:    noLock,
		skipLock:  noLock || skipLock(gopts),
		exclusive: false,
	}, printer)
}

func openWithAppendLock(ctx context.Context, gopts global.Options, dryRun bool, printer progress.Printer) (context.Context, *repository.Repository, func(), error) {
	return internalOpenWithLocked(ctx, gopts, openLockConfig{
		dryRun:    dryRun,
		skipLock:  skipLock(gopts),
		exclusive: false,
	}, printer)
}

func openWithExclusiveLock(ctx context.Context, gopts global.Options, dryRun bool, printer progress.Printer) (context.Context, *repository.Repository, func(), error) {
	return internalOpenWithLocked(ctx, gopts, openLockConfig{
		dryRun:    dryRun,
		skipLock:  skipLock(gopts),
		exclusive: true,
	}, printer)
}
