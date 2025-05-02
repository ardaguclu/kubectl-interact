package cmd

import (
	"context"
	"fmt"
	"github.com/ardaguclu/kubectl-interact/pkg/provider"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoogleCloudPlatform/kubectl-ai/gollm"
	"github.com/ardaguclu/kubectl-interact/pkg/agent"
	"github.com/ardaguclu/kubectl-interact/pkg/tools"
	"github.com/ardaguclu/kubectl-interact/pkg/ui"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

var (
	interactExample = `
	# Run predefined kubectl commands via given LLM model
	%[1]s interact
`
)

type InteractOptions struct {
	kubeConfig    string
	modelProvider string
	modelURL      string
	modelID       string
	apiKey        string
	caCert        string

	genericiooptions.IOStreams
}

// NewInteractOptions provides an instance of NamespaceOptions with default values
func NewInteractOptions(streams genericiooptions.IOStreams) *InteractOptions {
	return &InteractOptions{
		modelProvider: provider.GenericProvider,
		modelURL:      os.Getenv("MODEL_URL"),
		modelID:       os.Getenv("MODEL_ID"),
		apiKey:        os.Getenv("MODEL_API_KEY"),
		IOStreams:     streams,
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

	cmd.Flags().StringVar(&o.modelProvider, "model-provider", o.modelProvider, "The model provider to use, defaults to generic provider")
	cmd.Flags().StringVar(&o.modelURL, "model-url", o.modelURL, "URL of the model API. This is ignored if model-provider is other than generic")
	cmd.Flags().StringVar(&o.modelID, "model-id", o.modelID, "ID of the model")
	cmd.Flags().StringVar(&o.apiKey, "api-key", o.apiKey, "API Key of the model API")
	cmd.Flags().StringVar(&o.caCert, "ca-cert", o.caCert, "CA Cert path for the model API")
	cmd.Flags().StringVar(&o.kubeConfig, "kubeconfig", "", "path to the kubeconfig file")
	return cmd
}

func (o *InteractOptions) Complete() error {
	kubeconfigPath := o.kubeConfig
	if kubeconfigPath == "" {
		// Check environment variable
		kubeconfigPath = os.Getenv("KUBECONFIG")
		if kubeconfigPath == "" {
			// Use default path
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("error getting user home directory: %w", err)
			}
			kubeconfigPath = filepath.Join(homeDir, ".kube", "config")
		}
	}
	o.kubeConfig = kubeconfigPath

	return nil
}

func (o *InteractOptions) Validate() error {
	return nil
}

func (o *InteractOptions) Run() error {
	err := o.Generate(context.TODO())
	if err != nil {
		return err
	}
	return nil
}

func (o *InteractOptions) Generate(ctx context.Context) error {
	if o.modelProvider == provider.GenericProvider {
		ctx = context.WithValue(ctx, "url", o.modelURL)
		ctx = context.WithValue(ctx, "caCert", o.caCert)
		ctx = context.WithValue(ctx, "apiKey", o.apiKey)
	}
	llmClient, err := gollm.NewClient(ctx, o.modelProvider)
	if err != nil {
		return fmt.Errorf("creating llm client: %w", err)
	}
	defer llmClient.Close()

	doc := ui.NewDocument(o.IOStreams)

	u, err := ui.NewTerminalUI(doc)
	if err != nil {
		return err
	}

	conversation := &agent.Conversation{
		Model:      o.modelID,
		Kubeconfig: o.kubeConfig,
		LLM:        llmClient,
		Tools:      tools.Default(),
	}

	err = conversation.Init(ctx, doc, o.IOStreams)
	if err != nil {
		return fmt.Errorf("starting conversation: %w", err)
	}
	defer conversation.Close()

	chatSession := session{
		model:        o.modelID,
		doc:          doc,
		ui:           u,
		conversation: conversation,
		LLM:          llmClient,
		streams:      o.IOStreams,
	}

	return chatSession.repl(ctx)
}

// session represents the user chat session (interactive/non-interactive both)
type session struct {
	model           string
	ui              ui.UI
	doc             *ui.Document
	conversation    *agent.Conversation
	availableModels []string
	LLM             gollm.Client
	streams         genericiooptions.IOStreams
}

// repl is a read-eval-print loop for the chat session.
func (s *session) repl(ctx context.Context) error {
	query := "Hey there, what can I help you with today?"
	s.doc.AddBlock(ui.NewAgentTextBlock().SetText(query, s.streams), s.streams)
	for {
		if query == "" {
			input := ui.NewInputTextBlock()
			s.doc.AddBlock(input, s.streams)

			userInput, err := input.Observable().Wait()
			if err != nil {
				if err == io.EOF {
					// Use hit control-D, or was piping and we reached the end of stdin.
					// Not a "big" problem
					return nil
				}
				return fmt.Errorf("reading input: %w", err)
			}
			query = strings.TrimSpace(userInput)
		}

		switch {
		case query == "":
			continue
		case query == "reset":
			err := s.conversation.Init(ctx, s.doc, s.streams)
			if err != nil {
				return err
			}
		case query == "clear":
			s.ui.ClearScreen()
		case query == "exit" || query == "quit":
			// s.ui.RenderOutput(ctx, "Allright...bye.\n")
			return nil
		default:
			if err := s.answerQuery(ctx, query); err != nil {
				errorBlock := &ui.ErrorBlock{}
				errorBlock.SetText(fmt.Sprintf("Error: %v\n", err), s.streams)
				s.doc.AddBlock(errorBlock, s.streams)
			}
		}
		// Reset query to empty string so that we prompt for input again
		query = ""
	}
}

func (s *session) listModels(ctx context.Context) ([]string, error) {
	if s.availableModels == nil {
		modelNames, err := s.LLM.ListModels(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing models: %w", err)
		}
		s.availableModels = modelNames
	}
	return s.availableModels, nil
}

func (s *session) answerQuery(ctx context.Context, query string) error {
	switch {
	case query == "model":
		infoBlock := &ui.AgentTextBlock{}
		infoBlock.AppendText(fmt.Sprintf("Current model is `%s`\n", s.model), s.streams)
		s.doc.AddBlock(infoBlock, s.streams)

	case query == "models":
		models, err := s.listModels(ctx)
		if err != nil {
			return fmt.Errorf("listing models: %w", err)
		}
		infoBlock := &ui.AgentTextBlock{}
		infoBlock.AppendText("\n  Available models:\n", s.streams)
		infoBlock.AppendText(strings.Join(models, "\n"), s.streams)
		s.doc.AddBlock(infoBlock, s.streams)

	default:
		return s.conversation.RunOneRound(ctx, query)
	}
	return nil
}
