package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/tokfinity/infera/internal/config"
	"github.com/tokfinity/infera/internal/handler"
)

func main() {
	cfg := config.Load()
	r := handler.NewRouter()
	addr := ":" + cfg.Port
	fmt.Println("infera server listening on", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
