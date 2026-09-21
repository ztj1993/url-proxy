package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()

	// 1. 标准包 lodash 元数据
	mux.HandleFunc("/npm/lodash", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("ETag", `"mock-lodash-etag"`)
		w.WriteHeader(http.StatusOK)
		jsonDoc := `{
  "name": "lodash",
  "dist-tags": {
    "latest": "4.17.21"
  },
  "versions": {
    "4.17.21": {
      "name": "lodash",
      "version": "4.17.21",
      "dist": {
        "integrity": "sha512-v2kDEe57lecTulaDIuNTPy3Ry4gLGJ6Z1O3vE1krgXZNrsQ+LFTGHVxVjcXPs17LhbZVGedAJv8XZ1tvj5FvSg==",
        "shasum": "679591c564c3bffaae8454cf0b3df370c3d6911c",
        "tarball": "https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz"
      }
    }
  }
}`
		fmt.Fprint(w, jsonDoc)
		log.Printf("[mock-npm] served lodash metadata for %s", r.URL.Path)
	})

	// 2. 作用域包 @types/node 元数据
	mux.HandleFunc("/npm/@types/node", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		jsonDoc := `{
  "name": "@types/node",
  "dist-tags": {
    "latest": "20.11.0"
  },
  "versions": {
    "20.11.0": {
      "name": "@types/node",
      "version": "20.11.0",
      "dist": {
        "tarball": "https://registry.npmjs.org/@types/node/-/node-20.11.0.tgz"
      }
    }
  }
}`
		fmt.Fprint(w, jsonDoc)
		log.Printf("[mock-npm] served @types/node metadata for %s", r.URL.Path)
	})

	// 3. 点号包名 socket.io 元数据
	mux.HandleFunc("/npm/socket.io", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		jsonDoc := `{
  "name": "socket.io",
  "dist-tags": {
    "latest": "4.7.2"
  },
  "versions": {
    "4.7.2": {
      "name": "socket.io",
      "version": "4.7.2",
      "dist": {
        "tarball": "https://registry.npmjs.org/socket.io/-/socket.io-4.7.2.tgz"
      }
    }
  }
}`
		fmt.Fprint(w, jsonDoc)
		log.Printf("[mock-npm] served socket.io metadata for %s", r.URL.Path)
	})

	// 4. 版本指定端点 /npm/lodash/4.17.21
	mux.HandleFunc("/npm/lodash/4.17.21", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.npm.install-v1+json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		jsonDoc := `{
  "name": "lodash",
  "version": "4.17.21",
  "dist": {
    "tarball": "https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz"
  }
}`
		fmt.Fprint(w, jsonDoc)
		log.Printf("[mock-npm] served version-specific metadata for %s", r.URL.Path)
	})

	// 5. 模拟 lodash Tarball 压缩包二进制下载
	mux.HandleFunc("/npm/lodash/-/lodash-4.17.21.tgz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "BINARY_MOCK_PAYLOAD_FOR_LODASH_4.17.21_TGZ")
		log.Printf("[mock-npm] served tarball binary for %s", r.URL.Path)
	})

	// 6. 模拟 @types/node Tarball 压缩包二进制下载
	mux.HandleFunc("/npm/@types/node/-/node-20.11.0.tgz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "BINARY_MOCK_PAYLOAD_FOR_TYPES_NODE_20.11.0_TGZ")
		log.Printf("[mock-npm] served scoped tarball binary for %s", r.URL.Path)
	})

	// 7. 模拟 socket.io Tarball 压缩包二进制下载
	mux.HandleFunc("/npm/socket.io/-/socket.io-4.7.2.tgz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "BINARY_MOCK_PAYLOAD_FOR_SOCKET_IO_4.7.2_TGZ")
		log.Printf("[mock-npm] served dotted tarball binary for %s", r.URL.Path)
	})

	addr := ":9996"
	log.Printf("Mock npm official registry server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
