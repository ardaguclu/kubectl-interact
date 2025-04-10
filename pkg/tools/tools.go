package tools

import (
	"fmt"
	"github.com/ollama/ollama/api"
	"k8s.io/kubectl/pkg/cmd"
)

func GenerateKubectlCommandsAsTool() []api.Tool {
	var tools []api.Tool
	kubectl := cmd.NewDefaultKubectlCommand()
	for _, command := range kubectl.Commands() {
		tool := api.Tool{
			Type: "function",
			Function: api.ToolFunction{
				Name:        fmt.Sprintf("kubectl_%s", command.Name()),
				Description: fmt.Sprintf("%s\n%s\n%s\n", command.Long, command.Short, command.Example),
			},
		}
		tools = append(tools, tool)
	}
	return tools

	/*return []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "getCurrentWeather",
				Description: "Get the current weather in a given location",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type":        "string",
							"description": "The city and state, e.g. San Francisco, CA",
						},
						"unit": map[string]any{
							"type": "string",
							"enum": []string{"fahrenheit", "celsius"},
						},
					},
					"required": []string{"location"},
				},
			},
		},
	}*/
}
