package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/moehoshio/NekoLcServer/internal/api"
	"github.com/moehoshio/NekoLcServer/internal/config"
)

func parseFlags() *config.CLIFlags {
	flags := &config.CLIFlags{}
	
	flags.ConfigPath = flag.String("config_path", "", "Path to configuration files directory")
	flags.Port = flag.Int("port", 0, "Server port (overrides config)")
	flags.Debug = flag.Bool("debug", false, "Enable debug mode (overrides config)")
	flags.EnableAuth = flag.Bool("enable_auth", false, "Enable authentication (overrides config)")
	flags.JWTSecret = flag.String("jwt_secret", "", "JWT secret key (overrides config)")
	flags.DatabaseType = flag.String("database_type", "", "Database type: sqlite, mysql, file (overrides config)")
	flags.DatabasePath = flag.String("database_path", "", "Database connection path (overrides config)")
	flags.Reload = flag.Bool("reload", false, "Hot-reload configuration and exit")
	flags.Help = flag.Bool("help", false, "Show help message")
	
	flag.Parse()
	return flags
}

func showHelp() {
	fmt.Println("NekoLc Server - GoLang API Server")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  ./nekolc-server [options]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --config_path=PATH     Path to configuration files directory (default: ./configs)")
	fmt.Println("  --port=PORT           Server port (default: 8080)")
	fmt.Println("  --debug=BOOL          Enable debug mode (default: false)")
	fmt.Println("  --enable_auth=BOOL    Enable authentication (default: false)")
	fmt.Println("  --jwt_secret=SECRET   JWT secret key")
	fmt.Println("  --database_type=TYPE  Database type: sqlite, mysql, file (default: sqlite)")
	fmt.Println("  --database_path=PATH  Database connection path")
	fmt.Println("  --reload              Hot-reload configuration and exit")
	fmt.Println("  --help                Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ./nekolc-server --config_path=/path/to/configs --debug=true")
	fmt.Println("  ./nekolc-server --port=9000 --enable_auth=true")
	fmt.Println("  ./nekolc-server --reload")
	fmt.Println()
}

func main() {
	flags := parseFlags()
	
	if *flags.Help {
		showHelp()
		return
	}
	
	if *flags.Reload {
		fmt.Println("Hot-reloading configuration...")
		// TODO: Implement hot-reload functionality
		// For now, just validate and reload config
		cfg := config.LoadWithFlags(flags)
		fmt.Printf("Configuration reloaded successfully from: %s\n", cfg.ConfigPath)
		return
	}
	
	cfg := config.LoadWithFlags(flags)
	
	router := api.SetupRoutes(cfg)
	
	log.Printf("Starting NekoLc Server on port %s", cfg.App.Server.Port)
	log.Printf("Configuration loaded from: %s", cfg.ConfigPath)
	log.Printf("Database: %s at %s", cfg.App.Database.Type, cfg.App.Database.Path)
	if cfg.App.Authentication.Enabled {
		log.Printf("Authentication: enabled")
	}
	if cfg.App.Debug.Enabled {
		log.Printf("Debug mode: enabled")
	}
	
	if err := http.ListenAndServe(":"+cfg.App.Server.Port, router); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}