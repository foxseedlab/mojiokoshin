package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/foxseedlab/mojiokoshin/internal/config"
	"github.com/foxseedlab/mojiokoshin/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/do/v2"
)

const databaseInitTimeout = 15 * time.Second

func RegisterDI(injector do.Injector) {
	do.Provide(injector, func(i do.Injector) (repository.Repository, error) {
		cfg := do.MustInvoke[*config.Config](i)
		ctx, cancel := context.WithTimeout(context.Background(), databaseInitTimeout)
		defer cancel()

		p, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("failed to connect database: %w", err)
		}
		if err := p.Ping(ctx); err != nil {
			p.Close()
			return nil, fmt.Errorf("failed to ping database: %w", err)
		}
		if err := RunMigration(ctx, p); err != nil {
			p.Close()
			return nil, fmt.Errorf("failed to run migration: %w", err)
		}
		return NewPostgresRepository(p), nil
	})
}
