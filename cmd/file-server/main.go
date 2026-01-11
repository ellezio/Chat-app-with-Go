package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"
)

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
		cfg.dir = filepath.ToSlash(filepath.Clean(cfg.dir))
	}
	return nil
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	file, _, err := r.FormFile("file")
	if err != nil {
		fmt.Println("failed to parse file, err: ", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	buf := make([]byte, 512)
	if _, err := file.Read(buf); err != nil {
		fmt.Println("failed to read file, err: ", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	mime := http.DetectContentType(buf)
	if mime != "image/jpeg" {
		http.Error(w, "invalid file type - only jpeg supported", http.StatusBadRequest)
		return
	}

	defer func() {
		file.Close()
		r.MultipartForm.RemoveAll()
	}()

	filename := generateFilename() + ".jpeg"
	filepath := path.Join(cfg.dir, filename)
	dst, err := os.Create(filepath)
	if err != nil {
		fmt.Println("failed to create file, err: ", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	file.Seek(0, io.SeekStart)
	_, err = io.Copy(dst, file)
	dst.Close()
	if err != nil {
		fmt.Println("failed to copy file, err: ", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(filename))
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

	return fmt.Sprintf("%d%d", us, seq)
}

func main() {
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

	http.Handle("GET /", http.FileServer(http.Dir(cfg.dir)))
	http.HandleFunc("POST /", handleUpload)

	if err := http.ListenAndServe(":3001", nil); err != nil {
		panic(err)
	}
}
