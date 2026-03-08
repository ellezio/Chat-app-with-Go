package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ellezio/Chat-app-with-Go/internal/log"
)

var supportedFileFormat = []string{
	"jpeg",
	"png",
}

var mu sync.Mutex
var seqNum int64
var lastUnixSec int64

type config struct {
	// path to directory where the files will be stored
	dir string
}

var cfg config

func parseFlags() error {
	flag.StringVar(&cfg.dir, "dir", "", "path to directory where the files will be stored")
	flag.Parse()

	if cfg.dir == "" {
		return errors.New("the -dir flag is required")
	} else {
		cfg.dir = filepath.Clean(cfg.dir)
	}
	return nil
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	logger := log.Ctx(r.Context())

	file, _, err := r.FormFile("file")
	if err != nil {
		logger.Error("failed to parse file", slog.Any("error", err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	defer func() {
		file.Close()
		r.MultipartForm.RemoveAll()
	}()

	buf := make([]byte, 512)
	if _, err := io.ReadFull(file, buf); err != nil {
		logger.Error("failed to read file", slog.Any("error", err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	mediatype := http.DetectContentType(buf)
	mediaParts := strings.SplitN(mediatype, "/", 2)
	if len(mediaParts) != 2 || mediaParts[0] != "image" {
		http.Error(w, "invalid media type - only image are supported", http.StatusBadRequest)
		return
	}

	if !slices.Contains(supportedFileFormat, mediaParts[1]) {
		http.Error(w, "invalid media type - only image/jpeg and image/png are supported", http.StatusBadRequest)
		return
	}

	fname := generateFilename() + "." + mediaParts[1]
	fpath := filepath.Join(cfg.dir, fname)
	dst, err := os.Create(fpath)
	if err != nil {
		logger.Error("failed to create file", slog.Any("error", err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		dst.Close()
		os.Remove(fpath)
		logger.Error("failed to seek file", slog.Any("error", err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = io.Copy(dst, file)
	dst.Close()
	if err != nil {
		os.Remove(fpath)
		logger.Error("failed to copy file", slog.Any("error", err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(fname))
}

func generateFilename() string {
	mu.Lock()
	us := time.Now().Unix()
	var seq int64
	if us != lastUnixSec {
		seqNum = 0
		lastUnixSec = us
	}
	seq = seqNum
	seqNum++
	mu.Unlock()

	return fmt.Sprintf("%d_%d", us, seq)
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})).
		With("service", "file-server")
	log.DefaultContextLogger = logger

	if err := parseFlags(); err != nil {
		panic(err)
	}

	if _, err := os.Stat(cfg.dir); os.IsNotExist(err) {
		if err := os.MkdirAll(cfg.dir, 0755); err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /", handleUpload)
	mux.Handle("GET /{filename}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fname := r.PathValue("filename")
		// only accept file name not a path
		if fname != filepath.Base(fname) {
			http.Error(w, "invalid filename", http.StatusBadRequest)
			return
		}

		fpath := filepath.Join(cfg.dir, fname)
		http.ServeFile(w, r, fpath)
	}))

	if err := http.ListenAndServe(":3001", log.Middleware(mux, logger)); err != nil {
		panic(err)
	}
}
