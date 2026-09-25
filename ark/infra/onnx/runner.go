package onnx

// modelRunner defines the low-level backend contract for ONNX inference.
type modelRunner interface {
	Run(batch BatchInput) (BatchOutput, error)
	Close() error
}
