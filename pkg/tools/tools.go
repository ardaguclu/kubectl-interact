package tools

import (
	"fmt"

	"github.com/spf13/pflag"

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
		if !allowedCmdList(cmd.Name()) {
			continue
		}
		tool := ToolCall{
			Type: "function",
			Function: ToolFunction{
				Name:        fmt.Sprintf("kubectl__%s", cmd.Name()),
				Description: fmt.Sprintf("%s\n%s\n%s\n%s\n", cmd.Short, cmd.Use, cmd.Short, cmd.Example),
				Parameters:  make(map[string]interface{}),
			},
		}
		flags := cmd.Flags()
		flags.Visit(func(flag *pflag.Flag) {
			tool.Function.Parameters[flag.Name] = struct {
				Type        string `json:"type"`
				Description string `json:"description"`
			}{
				Type:        flag.Value.Type(),
				Description: flag.Usage,
			}
		})

		tools = append(tools, tool)
	}
	return tools
}

func allowedCmdList(cmdName string) bool {
	allowedCmds := []string{
		"annotate",
		"auth",
		"certificate",
		"cluster-info",
		"cp",
		"create",
		"describe",
		"diff",
		"drain",
		"events",
		"explain",
		"expose",
		"get",
		"label",
		"logs",
		"proxy",
		"run",
		"scale",
		"set",
		"taint",
		"top",
		"uncordon",
		"version",
	}
	for _, val := range allowedCmds {
		if val == cmdName {
			return true
		}
	}
	return false
}
