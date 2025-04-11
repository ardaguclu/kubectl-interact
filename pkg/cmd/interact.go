package cmd

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

	"github.com/spf13/cobra"

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
	completionEndpoint = "/v1/chat/completions"
)

type InteractOptions struct {
	configFlags *genericclioptions.ConfigFlags
	f           util.Factory

	modelAPI string
	modelID  string
	apiKey   string
	caCert   string

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
	return cmd
}

func (o *InteractOptions) Complete() error {
	o.f = util.NewFactory(o.configFlags)
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
	fmt.Fprintf(o.Out, "model-id: %s\n", o.apiKey)

	if o.modelAPI == "" || o.apiKey == "" || o.modelID == "" {
		return fmt.Errorf("please provide a valid api or model")
	}

	url := strings.TrimRight(o.modelAPI, "/") + completionEndpoint
	var messages []tools.Message

	fmt.Println("Kubectl Chatbot (type 'exit' to quit)")
	fmt.Println("========================================")

	scanner := bufio.NewScanner(o.In)
	client, err := o.getClient()
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

		messages = append(messages, tools.Message{
			Role:    "user",
			Content: userInput,
		}, tools.Message{
			Role: "system",
			Content: `
You are a helpful assistant with access to the following function calls. 
Your task is to produce a list of function calls necessary to generate response to the user utterance.
Use tools only if it is required. 
Execute as many tools as required to find out correct answer.
Use the following function calls as required.
`,
		})

		chatRequest := tools.ChatRequest{
			Model:       o.modelID,
			Messages:    messages,
			Temperature: 0,
			Tools:       tools.GenerateKubectlCommandsAsTool(),
		}

		requestBody, err := json.Marshal(chatRequest)
		if err != nil {
			fmt.Fprintf(o.ErrOut, "\nError creating request: %v\n", err)
			continue
		}

		req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
		if err != nil {
			fmt.Fprintf(o.ErrOut, "\nError creating request: %v\n", err)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+o.apiKey)

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(o.ErrOut, "\nError sending request: %v\n", err)
			continue
		}

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Fprintf(o.ErrOut, "\nError reading response: %v\n", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(o.ErrOut, "\nError: received status code %d: %s\n", resp.StatusCode, body)
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

func (o *InteractOptions) getClient() (*http.Client, error) {
	transport := &http.Transport{}
	if o.caCert != "" {
		ca, err := os.ReadFile(o.caCert)
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
