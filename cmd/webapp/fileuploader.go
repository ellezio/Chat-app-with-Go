package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/ellezio/Chat-app-with-Go/internal/log"
)

// TODO:
// - store connection here
// - palyaround with connection pool
// - tls
// - try also HTTP/2, gRPC
type FileUploader struct {
}

// Upload sends a file to the file server.
//
// Returns the uploaded file's name
func (fu *FileUploader) Upload(ctx context.Context, fname string, file io.Reader) (string, error) {
	body := &bytes.Buffer{}
	wr := multipart.NewWriter(body)
	fpartwr, err := wr.CreateFormFile("file", fname)
	if err != nil {
		return "", errors.Join(errors.New("can't create form file"), err)
	}
	_, err = io.Copy(fpartwr, file)
	if err != nil {
		wr.Close()
		return "", errors.Join(errors.New("failed to copy file to new request body"), err)
	}
	wr.Close()

	req, err := http.NewRequest("POST", "http://localhost:3001", body)
	req.Header.Add("Content-Type", wr.FormDataContentType())
	req.Header.Add(log.CorrelationIdHeader, log.CorrelationIdCtx(ctx))
	client := http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return "", errors.Join(errors.New("error while sending request"), err)
	}
	defer res.Body.Close()

	savedFilename, err := io.ReadAll(res.Body)
	if err != nil {
		return "", errors.Join(errors.New("error while reading response body"), err)
	}

	return string(savedFilename), nil
}
