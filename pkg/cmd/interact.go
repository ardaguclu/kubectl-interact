package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/ardaguclu/kubectl-interact/pkg/client"
	"github.com/ardaguclu/kubectl-interact/pkg/rag"
	"github.com/spf13/cobra"
	"net/http"
	"os"
	"strings"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/kubectl/pkg/cmd/util"

	"github.com/ardaguclu/kubectl-interact/pkg/tools"
)

var (
	interactExample = `
	# Run predefined kubectl commands via given LLM model
	%[1]s interact
`
)

const (
	completionEndpoint       = "/v1/chat/completions"
	ollamaCompletionEndpoint = "/api/chat"
)

type InteractOptions struct {
	configFlags *genericclioptions.ConfigFlags
	client      *http.Client
	f           util.Factory

	ollama   bool
	modelAPI string
	modelID  string
	apiKey   string
	caCert   string
	useRAG   bool

	genericiooptions.IOStreams
}

// NewInteractOptions provides an instance of NamespaceOptions with default values
func NewInteractOptions(streams genericiooptions.IOStreams) *InteractOptions {
	return &InteractOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		modelAPI:    os.Getenv("MODEL_API"),
		modelID:     os.Getenv("MODEL_ID"),
		apiKey:      os.Getenv("MODEL_API_KEY"),
		IOStreams:   streams,
	}
}

// NewCmdInteract provides a cobra command wrapping InteractOptions
func NewCmdInteract(streams genericiooptions.IOStreams) *cobra.Command {
	o := NewInteractOptions(streams)

	cmd := &cobra.Command{
		Use:          "interact",
		Short:        "interact",
		Example:      fmt.Sprintf(interactExample, "kubectl"),
		SilenceUsage: true,
		Annotations: map[string]string{
			cobra.CommandDisplayNameAnnotation: "kubectl interact",
		},
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if err := o.Run(); err != nil {
				return err
			}

			return nil
		},
	}

	o.configFlags.AddFlags(cmd.Flags())
	cmd.Flags().StringVar(&o.modelAPI, "model-api", o.modelAPI, "URL of the model API")
	cmd.Flags().StringVar(&o.modelID, "model-id", o.modelID, "ID of the model")
	cmd.Flags().StringVar(&o.apiKey, "api-key", o.apiKey, "API Key of the model API")
	cmd.Flags().StringVar(&o.caCert, "ca-cert", o.caCert, "CA Cert path for the model API")
	cmd.Flags().BoolVar(&o.useRAG, "use-rag", o.useRAG, "Enable this if your model supports embeddings")
	return cmd
}

func (o *InteractOptions) Complete() error {
	o.f = util.NewFactory(o.configFlags)

	if strings.Contains(o.modelAPI, "localhost") || strings.Contains(o.modelAPI, "127.0.0.1") {
		o.ollama = true
	}
	client, err := client.GetClient(o.caCert)
	if err != nil {
		return err
	}
	o.client = client
	return nil
}

func (o *InteractOptions) Validate() error {
	return nil
}

func (o *InteractOptions) Run() error {
	err := o.Generate()
	if err != nil {
		return err
	}
	return nil
}

func (o *InteractOptions) Generate() error {
	fmt.Fprintf(o.Out, "model-api: %s\n", o.modelAPI)
	fmt.Fprintf(o.Out, "model-id: %s\n", o.modelID)

	if o.modelAPI == "" || o.modelID == "" {
		return fmt.Errorf("please provide a valid api or model")
	}

	url := strings.TrimRight(o.modelAPI, "/")
	if o.ollama {
		url = url + ollamaCompletionEndpoint
	} else {
		url = url + completionEndpoint
	}

	fmt.Println("Kubectl Chatbot (type 'exit' to quit)")
	fmt.Println("========================================")

	scanner := bufio.NewScanner(o.In)
	for {
		fmt.Print("\nYou: ")
		if !scanner.Scan() {
			break
		}

		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "exit" || userInput == "quit" {
			break
		}

		if o.useRAG {
			commands, err := rag.SearchCommands(o.client, userInput, o.modelAPI, o.apiKey, o.modelID)
			if err != nil {
				fmt.Fprintf(o.ErrOut, "\nunexpected error during RAG search %v\n", err)
				continue
			}
			// TODO: embed this to the messages of system prompt
			fmt.Fprintf(o.Out, "\ncommands: %v\n", commands)
		}

		var messages []tools.Message
		messages = append(messages, tools.Message{
			Role:    "user",
			Content: userInput,
		}, tools.Message{
			Role: "system",
			Content: `
You are a helpful AI assistant with access to the following tools. When a tool is required to answer the user's query, respond with <|tool_call|> followed by a JSON list of tools used. If a tool does not exist in the provided list of tools, notify the user that you do not have the ability to fulfill the request.

Always try to add resource names in resource_name, resource types in resource_type.
Always add namespace. If it is not mentioned, it is default namespace. If it is mentioned all namespaces, it is all-namespaces=true.
`,
		})

		chatRequest := tools.ChatRequest{
			Model:       o.modelID,
			Messages:    messages,
			Temperature: 0.2,
		}

		if o.ollama {
			chatRequest.Tools = tools.GenerateKubectlCommandsAsToolOllama()
		} else {
			chatRequest.Tools = tools.GenerateKubectlCommandsAsTool()
		}

		requestBody, err := json.Marshal(chatRequest)
		if err != nil {
			fmt.Fprintf(o.ErrOut, "\nError creating request: %v\n", err)
			continue
		}

		body, err := client.Post(o.client, requestBody, url, o.apiKey)
		if err != nil {
			fmt.Fprintf(o.ErrOut, "\nError sending request: %v\n", err)
			continue
		}

		var chatResponse tools.ChatResponse
		if err := json.Unmarshal(body, &chatResponse); err != nil {
			fmt.Fprintf(o.ErrOut, "\nError parsing response: %v\n", err)
			continue
		}

		if len(chatResponse.Choices) == 0 {
			fmt.Fprintf(o.ErrOut, "\nError: No choices returned in response")
			continue
		}

		assistantMessage := chatResponse.Choices[0].Message
		messages = append(messages, assistantMessage)

		for _, toolCall := range assistantMessage.ToolCallResponses {
			if cmd, err := tools.ExecuteCommand(toolCall, o.IOStreams); err != nil {
				fmt.Fprintf(o.ErrOut, "\nError executing command: %v\n", err)
				break
			} else if cmd != "" {
				messages = append(messages, tools.Message{
					Role:    "user",
					Content: cmd,
				})
			}
		}
		if assistantMessage.Content != "" {
			fmt.Fprintf(o.Out, "Assistant: %s\n", assistantMessage.Content)
		}
	}
	return nil
}
