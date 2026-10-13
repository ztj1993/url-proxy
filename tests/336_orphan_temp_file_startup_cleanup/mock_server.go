package main

import (
	"net/http"
)

type MockOrphanHandler struct{}

func (h *MockOrphanHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/repo/package.deb" {
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mock deb binary payload"))
		return
	}
	http.NotFound(w, r)
}