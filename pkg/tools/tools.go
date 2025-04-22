package tools

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/pflag"
	"os"
	"os/exec"
	"strings"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/kubectl/pkg/cmd"
)

type Message struct {
	Role              string             `json:"role"`
	Content           string             `json:"content"`
	ToolCallResponses []ToolCallResponse `json:"tool_calls"`
}

type ChatRequest struct {
	Model       string     `json:"model"`
	Messages    []Message  `json:"messages"`
	Temperature float64    `json:"temperature"`
	Tools       []ToolCall `json:"tools"`
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

type ToolCall struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ToolCallResponse struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function ToolCallResponseDetail `json:"function"`
}

type ToolCallResponseDetail struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func GenerateKubectlCommandsAsTool() []ToolCall {
	var tools []ToolCall
	kubectl := cmd.NewDefaultKubectlCommand()
	for _, cmd := range kubectl.Commands() {
		tool := ToolCall{
			Type: "function",
			Function: ToolFunction{
				Name:        fmt.Sprintf("kubectl %s", cmd.Name()),
				Description: fmt.Sprintf("%s\n%s\n", cmd.Long, cmd.Example),
				Parameters:  make(map[string]interface{}),
			},
		}
		flags := cmd.Flags()
		flags.VisitAll(func(flag *pflag.Flag) {
			tool.Function.Parameters[flag.Name] = struct {
				Type        string `json:"type"`
				Description string `json:"description"`
			}{
				Type:        flag.Value.Type(),
				Description: flag.Usage,
			}
		})

		tool.Function.Parameters["resource_name"] = struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		}{
			Type:        "string",
			Description: "Name of a specific resource instance",
		}
		tool.Function.Parameters["resource_type"] = struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		}{
			Type:        "string",
			Description: "Kubernetes resource type (e.g. pod, deployment, service)",
		}

		tools = append(tools, tool)
	}
	return tools
}

func GenerateKubectlCommandsAsToolOllama() []ToolCall {
	var tools []ToolCall
	kubectl := cmd.NewDefaultKubectlCommand()
	for _, cmd := range kubectl.Commands() {
		tool := ToolCall{
			Type: "function",
			Function: ToolFunction{
				Name:        fmt.Sprintf("kubectl %s", cmd.Name()),
				Description: fmt.Sprintf("%s\n%s\n", cmd.Long, cmd.Example),
				Parameters:  make(map[string]interface{}),
			},
		}
		tool.Function.Parameters = make(map[string]any)
		tool.Function.Parameters["type"] = "object"
		tool.Function.Parameters["properties"] = make(map[string]any)
		flags := cmd.Flags()
		flags.VisitAll(func(flag *pflag.Flag) {
			tool.Function.Parameters["properties"].(map[string]any)[flag.Name] = struct {
				Type        string `json:"type"`
				Description string `json:"description"`
			}{
				Type:        flag.Value.Type(),
				Description: flag.Usage,
			}
		})
		tool.Function.Parameters["properties"].(map[string]any)["resource_name"] = struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		}{
			Type:        "string",
			Description: "Name of a specific resource instance",
		}
		tool.Function.Parameters["properties"].(map[string]any)["resource_type"] = struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		}{
			Type:        "string",
			Description: "Kubernetes resource type (e.g. pod, deployment, service)",
		}

		tools = append(tools, tool)
	}
	return tools
}

func ExecuteCommand(t ToolCallResponse, streams genericiooptions.IOStreams) (string, error) {
	cmdName := strings.ReplaceAll(t.Function.Name, "kubectl", "")
	params := make(map[string]any)
	json.Unmarshal([]byte(t.Function.Arguments), &params)
	var resourceType, resourceName string
	var args []string
	for key, val := range params {
		if key == "resource_type" {
			resourceType, _ = val.(string)
		} else if key == "resource_name" {
			resourceName, _ = val.(string)
		} else {
			args = append(args, fmt.Sprintf("--%s=%v", key, val))
		}
	}
	kubectl := cmd.NewDefaultKubectlCommand()
	for _, cmd := range kubectl.Commands() {
		if cmd.Name() != cmdName {
			continue
		}
		cmd.InheritedFlags()

		var prepend []string
		if resourceType != "" {
			prepend = append(prepend, resourceType)
		}
		if resourceName != "" {
			prepend = append(prepend, resourceName)
		}
		// TODO: detect flags and set in here, do not pass in args.
		args = append(prepend, args...)
		fmt.Fprintf(streams.Out, fmt.Sprintf("kubectl %s %s (Do you want to execute, y/n):", cmd.Name(), strings.Join(args, " ")))
		var input string
		_, err := fmt.Fscan(streams.In, &input)
		if err != nil {
			return "", err
		}

		if strings.EqualFold(input, "y") {
			final := append([]string{cmd.Name()}, args...)
			execution := exec.Command("kubectl", final...)

			// Set the output to stdout and stderr
			execution.Stdout = os.Stdout
			execution.Stderr = os.Stderr

			// Run the command
			if err := execution.Run(); err != nil {
				fmt.Println("Error:", err)
			}
			//cmd.Run(cmd, args)
		}
	}
	return "", nil
}
