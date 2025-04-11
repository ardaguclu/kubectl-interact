package llm

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"k8s.io/cli-runtime/pkg/genericiooptions"

	"github.com/ardaguclu/kubectl-interact/pkg/tools"
)

type Message struct {
	Role              string                   `json:"role"`
	Content           string                   `json:"content"`
	ToolCallResponses []tools.ToolCallResponse `json:"tool_calls"`
}

type ChatRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Temperature float64          `json:"temperature"`
	Tools       []tools.ToolCall `json:"tools"`
}

type MessageChoice struct {
	Message Message `json:"message"`
}

type MessageUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type ChatResponse struct {
	Choices []MessageChoice `json:"choices"`
	Usage   MessageUsage    `json:"usage"`
}

func Generate(api, apiKey, model, caCert string, streams genericiooptions.IOStreams) error {
	fmt.Fprintf(streams.Out, "model-api: %s\n", api)
	fmt.Fprintf(streams.Out, "model-id: %s\n", apiKey)

	if api == "" || apiKey == "" || model == "" {
		return fmt.Errorf("please provide a valid api or model")
	}

	url := strings.TrimRight(api, "/") + "/v1/chat/completions"
	var messages []Message

	fmt.Println("models-corp CLI Chatbot (type 'exit' to quit)")
	fmt.Println("========================================")

	scanner := bufio.NewScanner(streams.In)
	client, err := getClient(caCert)
	if err != nil {
		return err
	}

	for {
		fmt.Print("\nYou: ")
		if !scanner.Scan() {
			break
		}

		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "exit" || userInput == "quit" {
			break
		}

		messages = append(messages, Message{
			Role:    "user",
			Content: userInput,
		}, Message{
			Role: "system",
			Content: `
You are a helpful assistant with access to the following function calls. 
Your task is to produce a list of function calls necessary to generate response to the user utterance.
Use tools only if it is required. 
Execute as many tools as required to find out correct answer.
Use the following function calls as required.
`,
		})

		chatRequest := ChatRequest{
			Model:       model,
			Messages:    messages,
			Temperature: 0.7,
			Tools:       tools.GenerateKubectlCommandsAsTool(),
		}

		requestBody, err := json.Marshal(chatRequest)
		if err != nil {
			fmt.Fprintf(streams.ErrOut, "\nError creating request: %v\n", err)
			continue
		}

		req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
		if err != nil {
			fmt.Fprintf(streams.ErrOut, "\nError creating request: %v\n", err)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(streams.ErrOut, "\nError sending request: %v\n", err)
			continue
		}

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Fprintf(streams.ErrOut, "\nError reading response: %v\n", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(streams.ErrOut, "\nError: received status code %d: %s\n", resp.StatusCode, body)
			continue
		}

		var chatResponse ChatResponse
		if err := json.Unmarshal(body, &chatResponse); err != nil {
			fmt.Fprintf(streams.ErrOut, "\nError parsing response: %v\n", err)
			continue
		}

		if len(chatResponse.Choices) == 0 {
			fmt.Fprintf(streams.ErrOut, "\nError: No choices returned in response")
			continue
		}

		assistantMessage := chatResponse.Choices[0].Message
		messages = append(messages, assistantMessage)

		for _, toolCall := range assistantMessage.ToolCallResponses {
			fmt.Println(toolCall)
		}

		fmt.Fprintf(streams.Out, "\nAssistant: %s\n", assistantMessage.Content)
	}
	return nil
}

func getClient(caCert string) (*http.Client, error) {
	transport := &http.Transport{}
	if caCert != "" {
		ca, err := os.ReadFile(caCert)
		if err != nil {
			return nil, fmt.Errorf("unable to read CA cert: %w", err)
		}

		caCertPool, err := x509.SystemCertPool()
		if err != nil || caCertPool == nil {
			caCertPool = x509.NewCertPool()
		}
		caCertPool.AppendCertsFromPEM(ca)

		tlsConfig := &tls.Config{
			RootCAs: caCertPool,
		}

		transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
	}

	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}, nil
}
