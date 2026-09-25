package llamacpp

// modelRunner defines the low-level backend contract for tokenization, forward decode, and sampling.
type modelRunner interface {
	Tokenize(prompt string) ([]int32, error)
	Forward(tokens []int32, markerIndices []int) (map[int][]float32, error)
	SampleNext(tokens []int32, temp float32) (int32, string, error)
	DecodeStep(token int32, pos int32) error
	ClearKV() error
	TokenToPiece(token int32) (string, error)
	GetEOS() int32
	Close() error
}
