package acpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/qtopie/domour/ark/acp"
)

type stdioTransport struct {
	reader *bufio.Reader
	writer io.Writer
	closer io.Closer
}

func NewStdioTransport() *stdioTransport {
	return &stdioTransport{
		reader: bufio.NewReader(os.Stdin),
		writer: os.Stdout,
		closer: os.Stdin, // Dummy closer for stdin
	}
}

func (t *stdioTransport) ReadMessage(ctx context.Context) (*acp.JSONRPCRequest, error) {
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	var req acp.JSONRPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request: %w", err)
	}

	return &req, nil
}

func (t *stdioTransport) WriteMessage(ctx context.Context, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = t.writer.Write(append(data, '\n'))
	return err
}

func (t *stdioTransport) Close() error {
	return nil // Don't actually close stdin/stdout
}
