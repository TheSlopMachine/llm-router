package db

import (
	"errors"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"gorm.io/gorm"
)

// Translate maps GORM sentinel errors to domain sentinels.
// Matching uses errors.Is, never message text.
func Translate(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return apierrors.ErrNotFound
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return apierrors.ErrAlreadyExists
	case errors.Is(err, gorm.ErrForeignKeyViolated):
		return apierrors.ErrInvalidInput
	case errors.Is(err, gorm.ErrCheckConstraintViolated):
		return apierrors.ErrInvalidInput
	default:
		return err
	}
}
