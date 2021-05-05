package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/matryer/way"
	"github.com/nicolasparada/nakama/web"
)

func (h *handler) staticHandler() http.Handler {
	var root http.FileSystem
	if h.embedStaticFiles {
		sub, err := fs.Sub(web.Files, "static")
		if err != nil {
			_ = h.logger.Log("error", fmt.Errorf("could not embed static files: %w", err))
			os.Exit(1)
		}
		root = http.FS(sub)
	} else {
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			_ = h.logger.Log("error", "could not get runtime caller")
			os.Exit(1)
		}
		root = http.Dir(filepath.Join(path.Dir(file), "..", "..", "web", "static"))
	}
	return http.FileServer(&spaFileSystem{root: root})
}

type spaFileSystem struct {
	root http.FileSystem
}

func (fs *spaFileSystem) Open(name string) (http.File, error) {
	f, err := fs.root.Open(name)
	if os.IsNotExist(err) {
		return fs.root.Open("index.html")
	}
	return f, err
}

func (h *handler) avatar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := way.Param(ctx, "name")

	f, err := h.store.Open(ctx, name)
	if err != nil {
		h.respondErr(w, err)
		return
	}

	w.Header().Set("Content-Type", f.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(f.Size, 10))
	w.Header().Set("Etag", f.ETag)
	w.Header().Set("Last-Modified", f.LastModified.Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(w, f)
	if err != nil && !errors.Is(err, context.Canceled) {
		_ = h.logger.Log("error", fmt.Errorf("could not write down avatar: %w", err))
	}
}
