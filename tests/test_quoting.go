package main

import (
	"fmt"
	"os/exec"
)

func main() {
	prompt := "Why is the sky blue?"
	
	// My buggy code added literal quotes
	buggyPrompt := fmt.Sprintf("\"%s\"", prompt)
	
	fmt.Printf("Testing with buggy prompt: %s\n", buggyPrompt)
	
	cmd := exec.Command("echo", buggyPrompt)
	out, _ := cmd.CombinedOutput()
	fmt.Printf("Echo output (buggy): %s", string(out))

	fmt.Printf("Testing with clean prompt: %s\n", prompt)
	cmd2 := exec.Command("echo", prompt)
	out2, _ := cmd2.CombinedOutput()
	fmt.Printf("Echo output (clean): %s", string(out2))
}
