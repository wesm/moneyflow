package sqlite

import (
	"errors"

	"github.com/wesm/moneyflow/internal/store"
)

const (
	sqliteBusy    = 5
	sqliteLocked  = 6
	sqliteIOError = 10
	sqliteCorrupt = 11
	sqliteFull    = 13
	sqliteNotADB  = 26
	sqlitePrimary = 0xff
)

type sqliteCodedError interface {
	error
	Code() int
}

func mapDriverError(err error, fallback store.ErrorCode) error {
	if err == nil {
		return nil
	}
	var existing *store.Error
	if errors.As(err, &existing) {
		return err
	}
	var coded sqliteCodedError
	if errors.As(err, &coded) {
		switch coded.Code() & sqlitePrimary {
		case sqliteBusy, sqliteLocked:
			return store.NewError(store.CodeStoreBusy, err)
		case sqliteCorrupt, sqliteNotADB:
			return store.NewError(store.CodeStoreCorrupt, err)
		case sqliteIOError, sqliteFull:
			return store.NewError(store.CodeStoreError, err)
		}
	}
	return store.NewError(fallback, err)
}
