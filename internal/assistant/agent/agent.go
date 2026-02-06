package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"

	cfg "github.com/qtopie/homa/internal/app/config"
	"github.com/qtopie/homa/internal/assistant/plugins/copilot/shared"
	"github.com/qtopie/homa/internal/pkg/skill"
	"google.golang.org/genai"
)

// Agent represents the intelligent agent
type Agent struct {
	client *genai.Client
	skills []*skill.Skill
	model  string
}

// NewAgent creates a new agent
func NewAgent(ctx context.Context, apiKey string, modelName string) (*Agent, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, err
	}

	return &Agent{
		client: client,
		model:  modelName,
		skills: []*skill.Skill{},
	},
}

// LoadSkills loads skills from a directory
func (a *Agent) LoadSkills(dir string) error {
	matches, err := filepath.Glob(filepath.Join(dir, "*", "SKILL.md"))
	if err != nil {
		return err
	}

	for _, path := range matches {
		s, err := skill.ParseSkill(path)
		if err != nil {
			log.Printf("Failed to parse skill at %s: %v", path, err)
			continue
		}
		a.skills = append(a.skills, s)
		log.Printf("Loaded skill: %s", s.Name)
	}
	return nil
}

// Run executes the agent loop and streams the response

func (a *Agent) Run(ctx context.Context, req shared.UserRequest) (<-chan string, error) {

	outCh := make(chan string)

	

	go func() {

		defer close(outCh)



		// 1. Prepare Tools

		var tools []*genai.Tool

		toolFuncs := make(map[string]func(map[string]interface{}) (interface{}, error))



		for _, s := range a.skills {

			for _, t := range s.Tools {

				var params map[string]interface{}

				_ = json.Unmarshal(t.Parameters, &params)



				genaiTool := &genai.Tool{

					FunctionDeclarations: []*genai.FunctionDeclaration{{

						Name:        t.Name,

						Description: t.Description,

						Parameters:  params,

					}},

				}

				tools = append(tools, genaiTool)



				toolFuncs[t.Name] = func(args map[string]interface{}) (interface{}, error) {

					return fmt.Sprintf("Executed tool %s with args %v. Result: Valid SQL syntax.", t.Name, args), nil

				}

			}

		}



		// 2. Prepare System Prompt

		systemPrompt := "You are a helpful AI assistant."

		for _, s := range a.skills {

			systemPrompt += fmt.Sprintf("\n\nSkill: %s\n%s", s.Name, s.Instructions)

		}



		messages := []*genai.Content{

			{Role: "user", Parts: []*genai.Part{{Text: req.Message}}},

		}



		// Simplified ReAct Loop (Non-streaming internal steps, streaming final answer for now to avoid complexity)

		// Ideally we stream the "Thinking..." parts too.

		

		resp, err := a.client.Models.GenerateContent(ctx, a.model, messages[len(messages)-1].Parts[0], &genai.GenerateContentConfig{

			SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: systemPrompt}}},

			Tools:             tools,

		})

		

		if err != nil {

			outCh <- fmt.Sprintf("Error: %v", err)

			return

		}



		// Check for Function Calls (Simplistic check)

		// In a real implementation, we would check resp.Candidates[0].Content.Parts for FunctionCall

		// and loop back.

		

		// For now, just return the text

		if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {

			for _, part := range resp.Candidates[0].Content.Parts {

				if part.Text != "" {

					outCh <- part.Text

				}

			}

		}

	}()



	return outCh, nil

}


