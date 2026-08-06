package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	addr := flag.String("addr", ":8080", "Listen address")
	dir := flag.String("dir", "", "Static site directory (default: docs/ next to module or cwd)")
	flag.Parse()

	root := *dir
	if root == "" {
		root = findDocs()
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		log.Fatalf("docs directory not found: %s (pass -dir)", root)
	}

	abs, _ := filepath.Abs(root)
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(abs)))

	fmt.Printf("serving %s on http://localhost%s\n", abs, *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func findDocs() string {
	candidates := []string{"docs", filepath.Join("..", "docs")}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "docs"),
			filepath.Join(filepath.Dir(exe), "..", "docs"),
			filepath.Join(filepath.Dir(exe), "..", "..", "docs"),
		)
	}
	wd, _ := os.Getwd()
	for _, c := range candidates {
		p := c
		if !filepath.IsAbs(p) {
			p = filepath.Join(wd, c)
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return "docs"
}
