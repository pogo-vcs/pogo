package db

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pogo-vcs/pogo/server/env"
)

var (
	connPool *pgxpool.Pool
	Q        *Queries
	dbMutex  sync.Mutex
)

func Connect() {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if Q != nil {
		return
	}

	ctx := context.Background()

	config, err := pgxpool.ParseConfig(env.DatabaseUrl)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "failed to parse DATABASE_URL")
		os.Exit(1)
		return
	}
	config.MaxConns = 10
	config.MinConns = 2
	config.HealthCheckPeriod = time.Second * 10
	config.MaxConnLifetime = time.Second * 60
	config.MaxConnIdleTime = time.Second * 30

	for range 10 {
		connPool, err = pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "failed to connect to database")
			time.Sleep(time.Second)
			continue
		}

		err = connPool.Ping(ctx)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "failed to ping database")
			connPool.Close()
			time.Sleep(time.Second)
			continue
		}

		_, _ = fmt.Fprintln(os.Stdout, "connected to database")

		if err := Migrate(ctx, connPool); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "failed to migrate database:", err.Error())
			connPool.Close()
			os.Exit(1)
			return
		}

		Q = &Queries{
			db: connPool,
		}

		if err := Setup(ctx); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "failed to run setup:", err.Error())
			connPool.Close()
			os.Exit(1)
			return
		}

		return
	}

	_, _ = fmt.Fprintln(os.Stderr, "failed to connect to database after 10 attempts")
	os.Exit(1)
}

func Disconnect() {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if connPool != nil {
		connPool.Close()
		connPool = nil
		Q = nil
	}
}

type TxQueries struct {
	*Queries
	tx  pgx.Tx
	ctx context.Context
}

func (q TxQueries) Close() error {
	if q.db == nil {
		return nil
	}
	if tx, ok := q.db.(pgx.Tx); ok {
		return tx.Rollback(context.Background())
	}
	return errors.New("db was expected to be a pgx.Tx but it wasn't")
}

func (q TxQueries) Commit(ctx context.Context) error {
	if q.db == nil {
		return errors.New("db is nil")
	}
	if tx, ok := q.db.(pgx.Tx); ok {
		return tx.Commit(ctx)
	}
	return errors.New("db was expected to be a pgx.Tx but it wasn't")
}

func (q *Queries) Begin(ctx context.Context) (*TxQueries, error) {
	if q.db == nil {
		return nil, errors.New("db is nil")
	}
	switch db := q.db.(type) {
	case *pgxpool.Pool:
		tx, err := db.Begin(ctx)
		if err != nil {
			return nil, err
		}
		q := &Queries{
			db: tx,
		}
		return &TxQueries{
			Queries: q,
			tx:      tx,
			ctx:     ctx,
		}, nil
	case pgx.Tx:
		tx, err := db.Begin(ctx)
		if err != nil {
			return nil, err
		}
		q := &Queries{
			db: tx,
		}
		return &TxQueries{
			Queries: q,
			tx:      tx,
			ctx:     ctx,
		}, nil
	}
	return nil, errors.New("db is neither *pgxpool.Pool nor pgx.Tx")
}

const (
	changeIdAlphabet = "abcdefhkmnprwxyACDEFHJKLMNPRXY34"
	byteCount        = 10
)

var changeIdEncoding = base32.NewEncoding(changeIdAlphabet).WithPadding(base32.NoPadding)

func (r *Queries) GenerateChangeName(ctx context.Context, repositoryID int32) (string, error) {
	for range 16 {
		src := make([]byte, byteCount)
		_, _ = rand.Read(src)
		changeName := changeIdEncoding.EncodeToString(src)
		// check if it's already taken
		if _, err := r.FindChangeByNameExact(ctx, repositoryID, changeName); err != nil {
			if err == pgx.ErrNoRows {
				// it's available
				return changeName, nil
			}
			return "", fmt.Errorf("lookup if change name is taken: %w", err)
		} else {
			// it's taken
			fmt.Printf("change name %s is taken\n", changeName)
			continue
		}
	}
	return "", errors.New("too many attempts")
}
