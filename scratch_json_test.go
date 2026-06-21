package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/qtopie/domour/internal/app/assistant/shared"
)

func main() {
	event := shared.MotorStreamEvent{
		Stage: "reply",
		Done:  true,
		Err:   errors.New("test error"),
	}

	data, err := json.Marshal(event)
	if err != nil {
		fmt.Printf("Marshal error: %v\n", err)
		return
	}
	fmt.Printf("Serialized JSON: %s\n", string(data))

	var dec shared.MotorStreamEvent
	err = json.Unmarshal(data, &dec)
	if err != nil {
		fmt.Printf("Unmarshal error: %v\n", err)
		return
	}
	fmt.Printf("Unmarshal successful! Err: %v\n", dec.Err)
}
