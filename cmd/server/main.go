package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/moehoshio/NekoLcServer/internal/auth"
	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/localization"
	"github.com/moehoshio/NekoLcServer/internal/server"
)

func main() {
	var appConfigPath string
	flag.StringVar(&appConfigPath, "config", "./configs/app.json", "Path to app configuration file")
	flag.Parse()

	if env := os.Getenv("APP_CONFIG_PATH"); env != "" {
		appConfigPath = env
	}
	appConfigPath = filepath.Clean(appConfigPath)

	appCfg, err := config.LoadAppConfig(appConfigPath)
	if err != nil {
		log.Fatalf("load app config: %v", err)
	}

	baseDir := filepath.Dir(appConfigPath)
	resolve := func(path string) string {
		return resolvePath(baseDir, path)
	}

	languageBundle, err := config.LoadLanguages(resolve(appCfg.Language.ConfigPath))
	if err != nil {
		log.Fatalf("load languages: %v", err)
	}
	localizer := localization.New(appCfg.Language.Default, languageBundle)

	launcherCfg, err := config.LoadLauncher(resolve(appCfg.Launcher.ConfigPath))
	if err != nil {
		log.Fatalf("load launcher config: %v", err)
	}

	maintenanceCfg, err := config.LoadMaintenance(resolve(appCfg.Maintenance.ConfigPath))
	if err != nil {
		log.Fatalf("load maintenance config: %v", err)
	}

	updateConfigPath := resolve(appCfg.Update.ConfigPath)
	updateCfg, err := config.LoadUpdates(updateConfigPath)
	if err != nil {
		log.Fatalf("load update config: %v", err)
	}

	authSvc := auth.NewService(appCfg)

	feedbackPath := filepath.Join(baseDir, "logs", "feedback.log")
	updateAssetsDir := filepath.Dir(updateConfigPath)

	srv, err := server.New(appCfg, launcherCfg, maintenanceCfg, updateCfg, updateConfigPath, updateAssetsDir, localizer, authSvc, feedbackPath)
	if err != nil {
		log.Fatalf("bootstrap server: %v", err)
	}

	httpSrv := &http.Server{
		Addr:         fmt.Sprintf(":%s", appCfg.Server.Port),
		Handler:      srv.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("NekoLc server listening on %s", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-signalChan:
		log.Printf("received signal %s, shutting down", sig)
	case err := <-serverErrors:
		log.Fatalf("server error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Fatalf("graceful shutdown failed: %v", err)
	}
	log.Printf("server shutdown complete")
}

func resolvePath(baseDir, cfgPath string) string {
	if cfgPath == "" {
		return ""
	}
	if filepath.IsAbs(cfgPath) {
		return filepath.Clean(cfgPath)
	}
	cleaned := filepath.Clean(cfgPath)
	if _, err := os.Stat(cleaned); err == nil {
		return cleaned
	}
	combined := filepath.Join(baseDir, cleaned)
	if _, err := os.Stat(combined); err == nil {
		return combined
	}
	return combined
}
