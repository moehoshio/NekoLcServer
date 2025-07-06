package main

import (
	"log"
	"net/http"

	"github.com/moehoshio/NekoLcServer/internal/api"
	"github.com/moehoshio/NekoLcServer/internal/config"
)

func main() {
	cfg := config.Load()
	
	router := api.SetupRoutes(cfg)
	
	log.Printf("Starting NekoLc Server on port %s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}