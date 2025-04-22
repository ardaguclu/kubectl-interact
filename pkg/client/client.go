package client

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func Post(client *http.Client, requestBody []byte, url string, apiKey string) ([]byte, error) {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
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
	return body, nil
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
