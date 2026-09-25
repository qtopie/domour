//go:build !onnx

package onnx

func newModelRunner(cfg Config) (modelRunner, error) {
	return newStubRunner(cfg), nil
}
