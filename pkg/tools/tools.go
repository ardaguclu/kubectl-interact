package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/spf13/pflag"

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
				Name:        fmt.Sprintf("kubectl__%s", cmd.Name()),
				Description: fmt.Sprintf("%s\n%s\n", cmd.Short, cmd.Use),
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

		tools = append(tools, tool)
	}
	return tools
}

func GenerateKubectlCommandsAsToolOllama() []ToolCall {
	var tools []ToolCall
	kubectl := cmd.NewDefaultKubectlCommand()
	for _, cmd := range kubectl.Commands() {
		if cmd.Name() != "get" {
			continue
		}
		tool := ToolCall{
			Type: "function",
			Function: ToolFunction{
				Name:        fmt.Sprintf("kubectl__%s", cmd.Name()),
				Description: fmt.Sprintf("%s\n", cmd.Short),
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

		tools = append(tools, tool)
	}
	return tools
}

func ExecuteCommand(t ToolCallResponse, streams genericiooptions.IOStreams) (string, error) {
	cmdName := strings.ReplaceAll(t.Function.Name, "kubectl__", "")
	fmt.Fprintf(streams.Out, "Command %s is called as tool with args %v", cmdName, t.Function.Arguments)
	return "", nil
	/*params := make(map[string]map[string]string)
	json.Unmarshal([]byte(t.Function.Arguments), &params)
	arguments := params["arguments"]
	var args []string
	for key, val := range arguments {
		if isAllUpper(key) {
			args = append(args, val)
		} else {
			args = append(args, fmt.Sprintf("--%s=%s", key, val))
		}
	}
	kubectl := cmd.NewDefaultKubectlCommand()
	for _, cmd := range kubectl.Commands() {
		if cmd.Name() != cmdName {
			continue
		}
		cmd.InheritedFlags()

		fmt.Fprintf(streams.Out, fmt.Sprintf("kubectl %s %s (Do you want to execute, y/n):", cmd.Name(), strings.Join(args, " ")))
		var input string
		_, err := fmt.Fscan(streams.In, &input)
		if err != nil {
			return "", err
		}

		if strings.EqualFold(input, "y") {
			cmd.Run(cmd, args)
		}
	}
	return "", nil*/
}

func isAllUpper(s string) bool {
	hasLetter := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasLetter = true
			if !unicode.IsUpper(r) {
				return false
			}
		}
	}
	return hasLetter
}
