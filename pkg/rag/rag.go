package rag

import (
	"encoding/json"
	"fmt"
	"k8s.io/client-go/util/homedir"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"

	"k8s.io/kubectl/pkg/cmd"

	iclient "github.com/ardaguclu/kubectl-interact/pkg/client"
)

const (
	embeddingsEndpoint = "/v1/embeddings"
)

var (
	embeddingsCacheDir = homedir.HomeDir() + "/.kubectl-interact/embeddings"
)

func SearchCommands(client *http.Client, prompt string, url string, apiKey string, model string) (string, error) {
	embeddingFromQuestion, err := getEmbeddingFromChunk(client, prompt, "", url, apiKey, model, false)
	if err != nil {
		return "", err
	}
	var similarities []Similarity
	kubectl := cmd.NewDefaultKubectlCommand()
	for _, c := range kubectl.Commands() {
		if c.Example == "" {
			continue
		}
		chunks := []string{c.Example}
		for _, chunk := range chunks {
			embedding, err := getEmbeddingFromChunk(client, chunk, c.Name(), url, apiKey, model, true)
			if err != nil {
				return "", err
			}

			cosine, err := cosineSimilarity(embeddingFromQuestion, embedding)
			if err != nil {
				return "", err
			}

			bm25Score := calculateBM25Score(prompt, chunk) * 0.2

			totalScore := (cosine * 0.8) + bm25Score

			similarities = append(similarities, Similarity{
				Prompt: chunk,
				Score:  totalScore,
			})
		}
	}

	sort.Slice(similarities, func(i, j int) bool {
		return similarities[i].Score > similarities[j].Score
	})

	if len(similarities) == 0 {
		return "", nil
	}
	return similarities[0].Prompt, nil
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
	Prompt string
	Score  float64
}

func getEmbeddingFromChunk(client *http.Client, doc string, name string, url string, apiKey string, model string, useCache bool) ([]float64, error) {
	model = "granite3.1-moe:1b"
	if useCache {
		modelDir := fmt.Sprintf("%s/%s", embeddingsCacheDir, model)
		if _, err := os.Stat(modelDir); os.IsNotExist(err) {
			err := os.MkdirAll(modelDir, 0755)
			if err != nil {
				return nil, err
			}
		}

		f, err := os.ReadFile(fmt.Sprintf("%s/%s/%s", embeddingsCacheDir, model, name))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		} else if err == nil {
			var response EmbeddingResponse
			if err := json.Unmarshal(f, &response); err != nil {
				return nil, err
			}
			return response.Data[0].Embedding, nil
		}
	}

	request := EmbeddingRequest{
		Prompt: doc,
		Model:  model,
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	url = strings.TrimRight("http://127.0.0.1:11434", "/") + embeddingsEndpoint

	body, err := iclient.Post(client, requestBody, url, "")
	if err != nil {
		return nil, err
	}

	var response EmbeddingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if useCache {
		err = os.WriteFile(fmt.Sprintf("%s/%s/%s", embeddingsCacheDir, model, name), body, 0644)
		if err != nil {
			return nil, err
		}
	}

	return response.Data[0].Embedding, nil
}

// Add BM25 scoring for keyword matching
func calculateBM25Score(query string, chunk string) float64 {
	// Constants for BM25
	const k1 = 1.2
	const b = 0.75
	const avgDocLength = 500.0

	// Tokenize query and document
	queryTerms := strings.Fields(strings.ToLower(query))
	docTerms := strings.Fields(strings.ToLower(chunk))

	docLength := float32(len(docTerms))

	// Count term frequencies
	termFreq := make(map[string]int)
	for _, term := range docTerms {
		termFreq[term]++
	}

	// Calculate BM25 score
	var score float64
	for _, term := range queryTerms {
		if freq, exists := termFreq[term]; exists {
			// Calculate IDF - in a real implementation, this would use corpus statistics
			// Here we use a simplified approach
			idf := float32(1.0) // Simplified IDF

			// BM25 term scoring formula
			numerator := float32(freq) * (k1 + 1)
			denominator := float32(freq) + k1*(1-b+b*(docLength/avgDocLength))
			score += float64(idf * (numerator / denominator))
		}
	}

	return score
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
