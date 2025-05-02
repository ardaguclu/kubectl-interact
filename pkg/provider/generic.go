package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/GoogleCloudPlatform/kubectl-ai/gollm"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const GenericProvider = "generic"

func init() {
	gollm.RegisterProvider("generic", genericFactory)
}

func genericFactory(ctx context.Context, u *url.URL) (gollm.Client, error) {
	return NewGenericClient(ctx)
}

type GenericClient struct {
	client *http.Client
	url    string
	apiKey string
}

var _ gollm.Client = &GenericClient{}

func NewGenericClient(ctx context.Context) (*GenericClient, error) {
	u := ctx.Value("url").(string)
	caCert := ctx.Value("caCert").(string)
	apiKey := ctx.Value("apiKey").(string)

	client, err := GetClient(caCert)
	if err != nil {
		return nil, err
	}

	return &GenericClient{
		client: client,
		url:    u,
		apiKey: apiKey,
	}, nil
}

func GetClient(caCert string) (*http.Client, error) {
	transport := &http.Transport{}
	if caCert != "" {
		ca, err := os.ReadFile(caCert)
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
		Timeout:   5 * time.Minute,
		Transport: transport,
	}, nil
}

func (g GenericClient) Close() error {
	return nil
}

func (g GenericClient) StartChat(systemPrompt, model string) gollm.Chat {
	/*chat := fmt.Sprintf("%s/v1/chat/completions")
	req, err := http.NewRequest("POST", chat, bytes.NewBuffer(systemPrompt))
	if err != nil {
		return nil, fmt.Errorf("\nError creating request: %v\n", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("\nError sending request: %v\n", err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("\nError reading response: %v\n", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("\nError: received status code %d: %s\n", resp.StatusCode, body)
	}
	return body, nil*/
	//TODO implement me
	panic("implement me")
}

func (g GenericClient) GenerateCompletion(ctx context.Context, req *gollm.CompletionRequest) (gollm.CompletionResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (g GenericClient) SetResponseSchema(schema *gollm.Schema) error {
	//TODO implement me
	panic("implement me")
}

func (g GenericClient) ListModels(ctx context.Context) ([]string, error) {
	listModelUrl := fmt.Sprintf("%s/v1/models", g.url)
	req, err := http.NewRequest("GET", listModelUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("\nError creating request: %v\n", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.apiKey)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("\nError sending request: %v\n", err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("\nError reading response: %v\n", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("\nError: received status code %d: %s\n", resp.StatusCode, body)
	}
	return []string{string(body)}, nil
}
