// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"os"
	"sort"
	"strings"
	"time"

	_ "embed"

	"github.com/GoogleCloudPlatform/kubectl-ai/gollm"
	"k8s.io/klog/v2"

	"github.com/ardaguclu/kubectl-interact/pkg/tools"
	"github.com/ardaguclu/kubectl-interact/pkg/ui"
)

//go:embed systemprompt_template_default.txt
var defaultSystemPromptTemplate string

type Conversation struct {
	LLM gollm.Client

	Model string

	Tools tools.Tools

	Kubeconfig string

	// doc is the document which renders the conversation
	doc *ui.Document

	llmChat gollm.Chat

	workDir string

	streams genericiooptions.IOStreams
}

func (s *Conversation) Init(ctx context.Context, doc *ui.Document, streams genericiooptions.IOStreams) error {
	// Create a temporary working directory
	workDir, err := os.MkdirTemp("", "agent-workdir-*")
	if err != nil {
		return err
	}

	systemPrompt, err := s.generatePrompt(ctx, PromptData{
		Tools: s.Tools,
	})
	if err != nil {
		return fmt.Errorf("generating system prompt: %w", err)
	}

	// Start a new chat session
	s.llmChat = gollm.NewRetryChat(
		s.LLM.StartChat(systemPrompt, s.Model),
		gollm.RetryConfig{
			MaxAttempts:    3,
			InitialBackoff: 10 * time.Second,
			MaxBackoff:     60 * time.Second,
			BackoffFactor:  2,
			Jitter:         true,
		},
	)

	var functionDefinitions []*gollm.FunctionDefinition
	for _, tool := range s.Tools.AllTools() {
		functionDefinitions = append(functionDefinitions, tool.FunctionDefinition())
	}
	// Sort function definitions to help KV cache reuse
	sort.Slice(functionDefinitions, func(i, j int) bool {
		return functionDefinitions[i].Name < functionDefinitions[j].Name
	})
	if err := s.llmChat.SetFunctionDefinitions(functionDefinitions); err != nil {
		return fmt.Errorf("setting function definitions: %w", err)
	}

	s.streams = streams
	s.workDir = workDir
	s.doc = doc

	return nil
}

func (c *Conversation) Close() error {
	if c.workDir != "" {
		if err := os.RemoveAll(c.workDir); err != nil {
			klog.Warningf("error cleaning up directory %q: %v", c.workDir, err)
		}
	}
	return nil
}

// RunOneRound executes a chat-based agentic loop with the LLM using function calling.
func (c *Conversation) RunOneRound(ctx context.Context, query string) error {
	currChatContent := []any{query}

	currentIteration := 0
	maxIterations := 20

	for currentIteration < maxIterations {
		stream, err := c.llmChat.SendStreaming(ctx, currChatContent...)
		if err != nil {
			return err
		}

		// Clear our "response" now that we sent the last response
		currChatContent = nil

		// convert the candidate response into a gollm.ChatResponse
		stream, err = candidateToShimCandidate(stream)
		if err != nil {
			return err
		}

		// Process each part of the response
		// only applicable is not using tooluse shim
		var functionCalls []gollm.FunctionCall

		var agentTextBlock *ui.AgentTextBlock

		for response, err := range stream {
			if err != nil {
				return fmt.Errorf("reading streaming LLM response: %w", err)
			}
			if response == nil {
				// end of streaming response
				break
			}
			klog.Infof("response: %+v", response)

			if len(response.Candidates()) == 0 {
				return fmt.Errorf("no candidates in LLM response")
			}

			candidate := response.Candidates()[0]

			for _, part := range candidate.Parts() {
				// Check if it's a text response
				if text, ok := part.AsText(); ok {
					if agentTextBlock == nil {
						agentTextBlock = ui.NewAgentTextBlock()
						agentTextBlock.SetStreaming(true, c.streams)
						c.doc.AddBlock(agentTextBlock, c.streams)
					}
					agentTextBlock.AppendText(text, c.streams)
				}

				// Check if it's a function call
				if calls, ok := part.AsFunctionCalls(); ok && len(calls) > 0 {
					functionCalls = append(functionCalls, calls...)
				}
			}
		}

		if agentTextBlock != nil {
			agentTextBlock.SetStreaming(false, c.streams)
		}

		// TODO(droot): Run all function calls in parallel
		// (may have to specify in the prompt to make these function calls independent)
		for _, call := range functionCalls {
			toolCall, err := c.Tools.ParseToolInvocation(ctx, call.Name, call.Arguments)
			if err != nil {
				return fmt.Errorf("building tool call: %w", err)
			}

			s := toolCall.PrettyPrint()
			c.doc.AddBlock(ui.NewFunctionCallRequestBlock().SetText(fmt.Sprintf("  Running: %s\n", s), c.streams), c.streams)
			confirmationPrompt := `  Do you want to proceed ?
  1) Yes
  2) No`

			optionsBlock := ui.NewInputOptionBlock().SetPrompt(confirmationPrompt)
			optionsBlock.SetOptions([]string{"1", "2"})
			c.doc.AddBlock(optionsBlock, c.streams)

			selectedChoice, err := optionsBlock.Observable().Wait()
			if err != nil {
				if err == io.EOF {
					// Use hit control-D, or was piping and we reached the end of stdin.
					// Not a "big" problem
					return nil
				}
				return fmt.Errorf("reading input: %w", err)
			}

			switch selectedChoice {
			case "1":
				// Proceed with the operation
			case "2":
				c.doc.AddBlock(ui.NewAgentTextBlock().SetText("Operation was skipped.", c.streams), c.streams)
				observation := fmt.Sprintf("User didn't approve running %q.\n", call.Name)
				currChatContent = append(currChatContent, observation)
				continue
			default:
				// This case should technically not be reachable due to AskForConfirmation loop
				err := fmt.Errorf("invalid confirmation choice: %q", selectedChoice)
				c.doc.AddBlock(ui.NewErrorBlock().SetText("Invalid choice received. Cancelling operation.", c.streams), c.streams)
				return err
			}

			output, err := toolCall.InvokeTool(ctx, tools.InvokeToolOptions{
				Kubeconfig: c.Kubeconfig,
				WorkDir:    c.workDir,
			})
			if err != nil {
				return fmt.Errorf("executing action: %w", err)
			}

			observation := fmt.Sprintf("Result of running %q:\n%s", call.Name, output)
			currChatContent = append(currChatContent, observation)
		}

		// If no function calls were made, we're done
		if len(functionCalls) == 0 {
			return nil
		}

		currentIteration++
	}

	// If we've reached the maximum number of iterations
	errorBlock := ui.NewErrorBlock().SetText(fmt.Sprintf("Sorry, couldn't complete the task after %d iterations.\n", maxIterations, c.streams), c.streams)
	c.doc.AddBlock(errorBlock, c.streams)
	return fmt.Errorf("max iterations reached")
}

// toResult converts an arbitrary result to a map[string]any
func toResult(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("converting result to json: %w", err)
	}

	m := make(map[string]any)
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("converting json result to map: %w", err)
	}
	return m, nil
}

// generateFromTemplate generates a prompt for LLM. It uses the prompt from the provides template file or default.
func (a *Conversation) generatePrompt(_ context.Context, data PromptData) (string, error) {
	tmpl, err := template.New("promptTemplate").Parse(defaultSystemPromptTemplate)
	if err != nil {
		return "", fmt.Errorf("building template for prompt: %w", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, &data)
	if err != nil {
		return "", fmt.Errorf("evaluating template for prompt: %w", err)
	}
	return result.String(), nil
}

// PromptData represents the structure of the data to be filled into the template.
type PromptData struct {
	Query string
	Tools tools.Tools
}

func (a *PromptData) ToolsAsJSON() string {
	var toolDefinitions []*gollm.FunctionDefinition

	for _, tool := range a.Tools.AllTools() {
		toolDefinitions = append(toolDefinitions, tool.FunctionDefinition())
	}

	json, err := json.MarshalIndent(toolDefinitions, "", "  ")
	if err != nil {
		return ""
	}
	return string(json)
}

func (a *PromptData) ToolNames() string {
	return strings.Join(a.Tools.Names(), ", ")
}

type ReActResponse struct {
	Thought string  `json:"thought"`
	Answer  string  `json:"answer,omitempty"`
	Action  *Action `json:"action,omitempty"`
}

type Action struct {
	Name             string `json:"name"`
	Reason           string `json:"reason"`
	Command          string `json:"command"`
	ModifiesResource string `json:"modifies_resource"`
}

func extractJSON(s string) (string, bool) {
	const jsonBlockMarker = "```json"

	first := strings.Index(s, jsonBlockMarker)
	last := strings.LastIndex(s, "```")
	if first == -1 || last == -1 || first == last {
		return "", false
	}
	data := s[first+len(jsonBlockMarker) : last]

	return data, true
}

// parseReActResponse parses the LLM response into a ReActResponse struct
// This function assumes the input contains exactly one JSON code block
// formatted with ```json and ``` markers. The JSON block is expected to
// contain a valid ReActResponse object.
func parseReActResponse(input string) (*ReActResponse, error) {
	cleaned, found := extractJSON(input)
	if !found {
		return nil, fmt.Errorf("no JSON code block found in %q", cleaned)
	}

	cleaned = strings.ReplaceAll(cleaned, "\n", "")
	cleaned = strings.TrimSpace(cleaned)

	var reActResp ReActResponse
	if err := json.Unmarshal([]byte(cleaned), &reActResp); err != nil {
		return nil, fmt.Errorf("parsing JSON %q: %w", cleaned, err)
	}
	return &reActResp, nil
}

// toMap converts the value to a map, going via JSON
func toMap(v any) (map[string]any, error) {
	j, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("converting %T to json: %w", v, err)
	}
	m := make(map[string]any)
	if err := json.Unmarshal(j, &m); err != nil {
		return nil, fmt.Errorf("converting json to map: %w", err)
	}
	return m, nil
}

func candidateToShimCandidate(iterator gollm.ChatResponseIterator) (gollm.ChatResponseIterator, error) {
	return func(yield func(gollm.ChatResponse, error) bool) {
		buffer := ""
		for response, err := range iterator {
			if err != nil {
				yield(nil, err)
				return
			}

			if len(response.Candidates()) == 0 {
				yield(nil, fmt.Errorf("no candidates in LLM response"))
				return
			}

			candidate := response.Candidates()[0]

			for _, part := range candidate.Parts() {
				if text, ok := part.AsText(); ok {
					buffer += text
					klog.Infof("text is %q", text)
				} else {
					yield(nil, fmt.Errorf("no text part found in candidate"))
					return
				}
			}

			if _, found := extractJSON(buffer); found {
				break
			}
		}

		if buffer == "" {
			yield(nil, nil)
			return
		}

		parsedReActResp, err := parseReActResponse(buffer)
		if err != nil {
			yield(nil, fmt.Errorf("parsing ReAct response %q: %w", buffer, err))
			return
		}
		buffer = "" // TODO: any trailing text?
		yield(&ShimResponse{candidate: parsedReActResp}, nil)
	}, nil
}

type ShimResponse struct {
	candidate *ReActResponse
}

func (r *ShimResponse) UsageMetadata() any {
	return nil
}

func (r *ShimResponse) Candidates() []gollm.Candidate {
	return []gollm.Candidate{&ShimCandidate{candidate: r.candidate}}
}

type ShimCandidate struct {
	candidate *ReActResponse
}

func (c *ShimCandidate) String() string {
	return fmt.Sprintf("Thought: %s\nAnswer: %s\nAction: %s", c.candidate.Thought, c.candidate.Answer, c.candidate.Action)
}

func (c *ShimCandidate) Parts() []gollm.Part {
	var parts []gollm.Part
	if c.candidate.Thought != "" {
		parts = append(parts, &ShimPart{text: c.candidate.Thought})
	}
	if c.candidate.Answer != "" {
		parts = append(parts, &ShimPart{text: c.candidate.Answer})
	}
	if c.candidate.Action != nil {
		parts = append(parts, &ShimPart{action: c.candidate.Action})
	}
	return parts
}

type ShimPart struct {
	text   string
	action *Action
}

func (p *ShimPart) AsText() (string, bool) {
	return p.text, p.text != ""
}

func (p *ShimPart) AsFunctionCalls() ([]gollm.FunctionCall, bool) {
	if p.action != nil {
		functionCallArgs, err := toMap(p.action)
		if err != nil {
			return nil, false
		}
		delete(functionCallArgs, "name") // passed separately
		// delete(functionCallArgs, "reason")
		// delete(functionCallArgs, "modifies_resource")
		return []gollm.FunctionCall{
			{
				Name:      p.action.Name,
				Arguments: functionCallArgs,
			},
		}, true
	}
	return nil, false
}
