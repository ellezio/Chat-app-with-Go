package main

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/ellezio/Chat-app-with-Go/internal/log"
)

// TODO:
// - store connection here
// - palyaround with connection pool
// - tls
// - try also HTTP/2, gRPC
type FileUploader struct {
	host, port string
	client     *http.Client
}

func NewFileUploader(host, port string) *FileUploader {
	client := &http.Client{
		Transport: http.DefaultTransport,
		Timeout:   60 * time.Second,
	}

	return &FileUploader{host, port, client}
}

// Upload sends a file to the file server.
//
// Returns the uploaded file's name
func (fu *FileUploader) Upload(ctx context.Context, fname string, file io.Reader) (string, error) {
	pr, pw := io.Pipe()
	wr := multipart.NewWriter(pw)

	go func() {

		fpartwr, err := wr.CreateFormFile("file", fname)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("creating form file: %w", err))
			return
		}

		_, err = io.Copy(fpartwr, file)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("copying file to multipart: %w", err))
			return
		}

		if err := wr.Close(); err != nil {
			pw.CloseWithError(fmt.Errorf("closing multipart writer: %w", err))
			return
		}

		pw.Close()
	}()

	url := url.URL{Scheme: "http", Host: net.JoinHostPort(fu.host, fu.port)}
	req, err := http.NewRequestWithContext(ctx, "POST", url.String(), pr)
	if err != nil {
		pr.CloseWithError(err)
		return "", fmt.Errorf("creating request: %w", err)
	}

	if cid := log.CorrelationIdCtx(ctx); cid != "" {
		req.Header.Set(log.CorrelationIdHeader, cid)
	}

	req.Header.Set("Content-Type", wr.FormDataContentType())

	res, err := fu.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("unexpected status %d with body %s", res.StatusCode, body)
	}

	savedFilename, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("reading response body: %w", err)
	}

	return string(savedFilename), nil
}
