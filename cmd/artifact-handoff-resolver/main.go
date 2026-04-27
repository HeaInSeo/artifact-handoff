package main

import (
	"log"
	"net/http"
	"os"

	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
	"github.com/HeaInSeo/artifact-handoff/pkg/resolver"
)

func main() {
	addr := ":8080"
	if envAddr := os.Getenv("AH_ADDR"); envAddr != "" {
		addr = envAddr
	}

	store := inventory.NewMemoryStore()
	service := resolver.NewService(store)
	handler := resolver.NewHTTPHandler(service)

	log.Printf("artifact-handoff resolver listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}
