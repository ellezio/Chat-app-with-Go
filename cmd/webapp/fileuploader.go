package main

import (
	"bytes"
	"context"
	"errors"
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
	body := &bytes.Buffer{}

	wr := multipart.NewWriter(body)
	fpartwr, err := wr.CreateFormFile("file", fname)
	if err != nil {
		wr.Close()
		return "", errors.Join(errors.New("can't create form file"), err)
	}
	_, err = io.Copy(fpartwr, file)
	if err != nil {
		wr.Close()
		return "", errors.Join(errors.New("failed to copy file to new request body"), err)
	}
	wr.Close()

	url := url.URL{Scheme: "http", Host: net.JoinHostPort(fu.host, fu.port)}
	req, err := http.NewRequestWithContext(ctx, "POST", url.String(), body)
	if err != nil {
		return "", errors.Join(errors.New("failed to create request"), err)
	}

	if cid := log.CorrelationIdCtx(ctx); cid != "" {
		req.Header.Set(log.CorrelationIdHeader, cid)
	}

	req.Header.Set("Content-Type", wr.FormDataContentType())

	res, err := fu.client.Do(req)
	if err != nil {
		return "", errors.Join(errors.New("error while sending request"), err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("unexpected status %d with body %s", res.StatusCode, body)
	}

	savedFilename, err := io.ReadAll(res.Body)
	if err != nil {
		return "", errors.Join(errors.New("error while reading response body"), err)
	}

	return string(savedFilename), nil
}
