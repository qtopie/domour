package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	dapr "github.com/dapr/go-sdk/client"
)

// DaprActorChatModel implements model.ChatModel by calling a Dapr ModelActor.
type DaprActorChatModel struct {
	model  string
	client dapr.Client
}

func newDaprActorChatModel(ctx context.Context, cfg *Config) (model.ChatModel, error) {
	modelName := strings.TrimSpace(cfg.Model)
	if modelName == "" {
		modelName = "gemma-2-2b"
	}
	daprClient, err := dapr.NewClient()
	if err != nil {
		return nil, fmt.Errorf("create dapr client: %w", err)
	}
	return &DaprActorChatModel{
		model:  modelName,
		client: daprClient,
	}, nil
}

// BindTools is a no-op for DaprActorChatModel.
func (m *DaprActorChatModel) BindTools(tools []*schema.ToolInfo) error {
	return nil
}

// Generate sends messages to the ModelActor's ChatCompletions method using OpenAI compatible payload.
func (m *DaprActorChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	openAIMessages := make([]openaiMessage, 0, len(input))
	for _, msg := range input {
		role := string(msg.Role)
		if role == "" {
			role = "user"
		}
		openAIMessages = append(openAIMessages, openaiMessage{
			Role:    role,
			Content: msg.Content,
		})
	}

	reqPayload := openaiRequest{
		Model:    m.model,
		Messages: openAIMessages,
	}

	reqBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}

	resp, err := m.client.InvokeActor(ctx, &dapr.InvokeActorRequest{
		ActorType: "ModelActor",
		ActorID:   m.model,
		Method:    "ChatCompletions",
		Data:      reqBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("invoke model actor ChatCompletions: %w", err)
	}

	var oResp openaiResponse
	if err := json.Unmarshal(resp.Data, &oResp); err != nil {
		return nil, fmt.Errorf("unmarshal openai response: %w", err)
	}

	if len(oResp.Choices) == 0 {
		return nil, fmt.Errorf("model actor returned empty choices")
	}

	choice := oResp.Choices[0]
	respMsg := schema.AssistantMessage(choice.Message.Content, nil)
	return respMsg, nil
}

// Stream falls back to unary Generate call as Dapr Actor method invocation is strictly unary.
func (m *DaprActorChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	resp, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{resp}), nil
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
}

type openaiResponse struct {
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}
