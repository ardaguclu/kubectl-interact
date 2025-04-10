package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"strings"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"

	"github.com/ardaguclu/kubectl-interact/pkg/llm"
)

var (
	namespaceExample = `
	# view the current namespace in your KUBECONFIG
	%[1]s ns

	# view all of the namespaces in use by contexts in your KUBECONFIG
	%[1]s ns --list

	# switch your current-context to one that contains the desired namespace
	%[1]s ns foo
`
)

// InteractOptions provides information required to update
// the current context on a user's KUBECONFIG
type InteractOptions struct {
	configFlags *genericclioptions.ConfigFlags

	port  int
	model string

	genericiooptions.IOStreams
}

// NewInteractOptions provides an instance of NamespaceOptions with default values
func NewInteractOptions(streams genericiooptions.IOStreams) *InteractOptions {
	return &InteractOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		port:        11434,
		model:       "llama3.2",
		IOStreams:   streams,
	}
}

// NewCmdInteract provides a cobra command wrapping InteractOptions
func NewCmdInteract(streams genericiooptions.IOStreams) *cobra.Command {
	o := NewInteractOptions(streams)

	cmd := &cobra.Command{
		Use:          "interact",
		Short:        "interact",
		Example:      fmt.Sprintf(namespaceExample, "kubectl"),
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
			if err := o.Run(args); err != nil {
				return err
			}

			return nil
		},
	}

	o.configFlags.AddFlags(cmd.Flags())
	cmd.Flags().IntVar(&o.port, "port", o.port, "Defaults to 11434 as it is Ollama's default port number")
	cmd.Flags().StringVar(&o.model, "model", o.model, "The model is used in Ollama")
	return cmd
}

// Complete sets all information required for updating the current context
func (o *InteractOptions) Complete() error {
	/*config*/ _, err := o.configFlags.ToRESTConfig()
	if err != nil {
		return err
	}

	return nil
}

// Validate ensures that all required arguments and flag values are provided
func (o *InteractOptions) Validate() error {
	return nil
}

// Run lists all available namespaces on a user's KUBECONFIG or updates the
// current context based on a provided namespace.
func (o *InteractOptions) Run(args []string) error {
	err := llm.GenerateOllama(o.model, strings.Join(args, " "), o.port)
	if err != nil {
		return err
	}
	//ctx := context.Background()
	/*err := o.llm.Generate(ctx, strings.Join(args, " "))
	if err != nil {
		return err
	}*/
	return nil
}
