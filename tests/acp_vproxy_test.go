package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"testing"
	"time"
)

// ACP Message structures for client
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   interface{}     `json:"error,omitempty"`
}

func TestAgyVProxyProxyMode(t *testing.T) {
	fmt.Println("=== Testing Domour ACP Server: Agy-CLI + vproxy (Proxy Mode) ===")

	// 1. Build the domour binary first to ensure we are testing the latest code
	fmt.Println("[Step 1] Building domour binary...")
	buildCmd := exec.Command("go", "build", "-o", "domour_test_bin", "../cmd/main.go")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build domour: %v", err)
	}
	defer exec.Command("rm", "domour_test_bin").Run()

	// 2. Start the Domour ACP Server in a sub-process
	fmt.Println("[Step 2] Starting ACP Server...")
	cmd := exec.Command("./domour_test_bin", "acp")
	
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("Failed to get stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to get stdout: %v", err)
	}
	
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer cmd.Process.Kill()

	// Forward Stderr so we can see the [CLI Debug] logs
	cmd.Stderr = os.Stderr

	reader := bufio.NewReader(stdout)

	// 3. Handshake (Initialize) - Request Proxy Mode
	fmt.Println("[Step 3] Sending Initialize Request (mode: proxy)...")
	initReq := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: json.RawMessage(`{
			"capabilities": {
				"experimental": {
					"domourMode": "proxy"
				}
			},
			"clientInfo": {"name": "vproxy-test-client", "version": "1.0"}
		}`),
	}
	sendMsg(stdin, initReq)

	initResp := readMsg(reader)
	fmt.Printf("<<< Server Response: %s\n", string(initResp.Result))

	// 4. Send Prompt (This will trigger agy-cli via vproxy)
	fmt.Println("[Step 4] Sending Prompt to Agy-CLI...")
	promptReq := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "prompts/get",
		Params: json.RawMessage(`{
			"name": "chat",
			"arguments": {
				"content": "Why is the sky blue?"
			}
		}`),
	}
	sendMsg(stdin, promptReq)

	// Wait for response (agy-cli via vproxy might take a few seconds)
	fmt.Println("[Step 5] Waiting for Agy-CLI response (check backend logs for vproxy wrapping)...")
	
	select {
	case finalResp := <-readMsgAsync(reader):
		if finalResp.Error != nil {
			fmt.Printf("<<< Error from Server: %v\n", finalResp.Error)
		} else {
			fmt.Printf("<<< Final Response (Success!): %s\n", string(finalResp.Result))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Timeout waiting for Agy-CLI response")
	}
}

func sendMsg(w io.Writer, msg JSONRPCMessage) {
	data, _ := json.Marshal(msg)
	w.Write(append(data, '\n'))
}

func readMsg(r *bufio.Reader) JSONRPCMessage {
	line, _, err := r.ReadLine()
	if err != nil {
		log.Printf("Read error: %v", err)
		return JSONRPCMessage{}
	}
	var msg JSONRPCMessage
	json.Unmarshal(line, &msg)
	return msg
}

func readMsgAsync(r *bufio.Reader) <-chan JSONRPCMessage {
	ch := make(chan JSONRPCMessage)
	go func() {
		ch <- readMsg(r)
	}()
	return ch
}
