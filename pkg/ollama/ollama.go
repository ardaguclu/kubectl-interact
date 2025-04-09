package ollama

import (
	"context"
	"fmt"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"os"

	"github.com/ollama/ollama/api"
)

func Pull(port int, model string, streams genericiooptions.IOStreams) error {
	os.Setenv("OLLAMA_HOST", fmt.Sprintf("127.0.0.1:%d", port))
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return err
	}

	ctx := context.Background()

	req := &api.PullRequest{
		Model: model,
	}
	progressFunc := func(resp api.ProgressResponse) error {
		fmt.Fprintf(streams.Out, "Progress: status=%v, total=%v, completed=%v\n", resp.Status, resp.Total, resp.Completed)
		return nil
	}

	err = client.Pull(ctx, req, progressFunc)
	if err != nil {
		return err
	}

	return nil
}

func Chat(model string, streams genericiooptions.IOStreams) error {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return err
	}

	messages := []api.Message{
		api.Message{
			Role:    "system",
			Content: "Provide very brief, concise responses",
		},
		api.Message{
			Role:    "user",
			Content: "Name some unusual animals",
		},
		api.Message{
			Role:    "assistant",
			Content: "Monotreme, platypus, echidna",
		},
		api.Message{
			Role:    "user",
			Content: "which of these is the most dangerous?",
		},
	}

	ctx := context.Background()
	req := &api.ChatRequest{
		Model:    model,
		Messages: messages,
	}

	respFunc := func(resp api.ChatResponse) error {
		fmt.Println(resp)
		fmt.Fprintf(streams.ErrOut, resp.Message.Content)
		return nil
	}

	return client.Chat(ctx, req, respFunc)
}
