package dolyami

import "errors"

var (
	ErrConfigurationMissing = errors.New("dolyami: configuration missing")
	ErrInvalidConfiguration = errors.New("dolyami: invalid configuration")
)
