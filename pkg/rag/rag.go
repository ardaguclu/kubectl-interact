package rag

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"math"
	"net/http"
	"sort"
	"strings"

	"k8s.io/kubectl/pkg/cmd"

	iclient "github.com/ardaguclu/kubectl-interact/pkg/client"
)

const embeddingsEndpoint = "/v1/embeddings"

func SearchCommands(client *http.Client, prompt string, url string, apiKey string, model string) (string, error) {
	var vectorStore []VectorRecord
	kubectl := cmd.NewDefaultKubectlCommand()
	for _, c := range kubectl.Commands() {
		if c.Example == "" {
			continue
		}
		//chunks := chunkText(fmt.Sprintf("Example: %s", c.Example), 256, 256)
		fmt.Printf("CMD: %s CHUNK SIZE: %d\n", c.Name(), len(c.Example))
		chunks := []string{c.Example}
		for _, chunk := range chunks {

			embedding, err := getEmbeddingFromChunk(client, chunk, url, apiKey, model)
			if err != nil {
				return "", err
			}

			record := VectorRecord{
				Command:   *c,
				Prompt:    chunk,
				Embedding: embedding,
			}
			vectorStore = append(vectorStore, record)
		}
	}

	embeddingFromQuestion, err := getEmbeddingFromChunk(client, prompt, url, apiKey, model)
	if err != nil {
		return "", err
	}

	var similarities []Similarity
	for _, vector := range vectorStore {
		cosine, err := cosineSimilarity(embeddingFromQuestion, vector.Embedding)
		if err != nil {
			return "", err
		}

		similarities = append(similarities, Similarity{
			Prompt:           vector.Command.Example,
			CosineSimilarity: cosine,
		})
	}

	sort.Slice(similarities, func(i, j int) bool {
		return similarities[i].CosineSimilarity > similarities[j].CosineSimilarity
	})

	if len(similarities) == 0 {
		return "", nil
	}
	return similarities[0].Prompt, nil
}

type VectorRecord struct {
	Command   cobra.Command
	Prompt    string    `json:"prompt"`
	Embedding []float64 `json:"embedding"`
}

type EmbeddingRequest struct {
	Prompt string `json:"input"`
	Model  string `json:"model"`
}

type EmbeddingResponse struct {
	Model string                  `json:"model"`
	Data  []EmbeddingDataResponse `json:"data"`
}

type EmbeddingDataResponse struct {
	Embedding []float64 `json:"embedding"`
}

type Similarity struct {
	Prompt           string
	CosineSimilarity float64
}

func getEmbeddingFromChunk(client *http.Client, doc string, url string, apiKey string, model string) ([]float64, error) {
	request := EmbeddingRequest{
		Prompt: doc,
		Model:  model,
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	url = strings.TrimRight(url, "/") + embeddingsEndpoint

	body, err := iclient.Post(client, requestBody, url, apiKey)
	if err != nil {
		return nil, err
	}

	var response EmbeddingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	return response.Data[0].Embedding, nil
}

func cosineSimilarity(vec1, vec2 []float64) (float64, error) {
	if len(vec1) != len(vec2) {
		return 0, fmt.Errorf("vectors must have the same length")
	}

	dotProduct := 0.0
	magnitude1 := 0.0
	magnitude2 := 0.0

	for i := 0; i < len(vec1); i++ {
		dotProduct += vec1[i] * vec2[i]
		magnitude1 += vec1[i] * vec1[i]
		magnitude2 += vec2[i] * vec2[i]
	}

	magnitude1 = math.Sqrt(magnitude1)
	magnitude2 = math.Sqrt(magnitude2)

	if magnitude1 == 0 || magnitude2 == 0 {
		return 0, fmt.Errorf("vector magnitude cannot be zero")
	}

	return dotProduct / (magnitude1 * magnitude2), nil
}
