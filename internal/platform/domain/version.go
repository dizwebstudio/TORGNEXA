package domain

var version = "0.1.0-dev"

// Version returns the immutable build version injected by the release pipeline.
func Version() string {
	return version
}
