//go:build !llamacpp

package llamacpp

func newModelRunner(cfg Config) (modelRunner, error) {
	return &stubRunner{}, nil
}
