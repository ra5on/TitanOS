package config

import (
	"errors"
	"syscall"
)

func isInvalid(err error) bool {
	return errors.Is(err, syscall.EINVAL)
}
