package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ardaguclu/kubectl-interact/pkg/tools"
	"github.com/ollama/ollama/api"
	"os"
)

func GenerateOllama(model, message string, port int) error {
	ctx := context.Background()

	os.Setenv("OLLAMA_HOST", fmt.Sprintf("127.0.0.1:%d", port))
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return err
	}

	toolList := tools.GenerateKubectlCommandsAsTool()

	messages := []api.Message{
		{Role: "system", Content: "Always generate kubectl commands. lways use a tool"},
		{Role: "user", Content: message},
	}

	f := false
	req := &api.ChatRequest{
		Model:    model,
		Messages: messages,
		Options: map[string]interface{}{
			"temperature":   0.0,
			"repeat_last_n": 2,
		},
		Tools:  toolList,
		Stream: &f,
	}

	err = client.Chat(ctx, req, func(resp api.ChatResponse) error {
		r, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Println(string(r))
		for _, toolCall := range resp.Message.ToolCalls {
			fmt.Println(toolCall.Function.Name, toolCall.Function.Arguments)
		}

		return nil
	})

	if err != nil {
		return err
	}
	return nil
}
