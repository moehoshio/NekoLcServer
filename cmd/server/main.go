package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/moehoshio/NekoLcServer/internal/auth"
	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/localization"
	"github.com/moehoshio/NekoLcServer/internal/server"
	"github.com/moehoshio/NekoLcServer/internal/store"
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

	maintenanceConfigPath := resolve(appCfg.Maintenance.ConfigPath)
	maintenanceCfg, err := config.LoadMaintenance(maintenanceConfigPath)
	if err != nil {
		log.Fatalf("load maintenance config: %v", err)
	}

	newsConfigPath := resolve(appCfg.News.ConfigPath)
	newsCfg, err := config.LoadNews(newsConfigPath)
	if err != nil {
		log.Fatalf("load news config: %v", err)
	}

	updateConfigPath := resolve(appCfg.Update.ConfigPath)
	updateCfg, err := config.LoadUpdates(updateConfigPath)
	if err != nil {
		log.Fatalf("load update config: %v", err)
	}

	var st store.Store
	if strings.EqualFold(appCfg.Authentication.Method, "mysql") {
		mysqlCfg := store.MySQLConfig{
			Host:     appCfg.Authentication.MySQL.Host,
			Port:     appCfg.Authentication.MySQL.Port,
			Username: appCfg.Authentication.MySQL.Username,
			Password: appCfg.Authentication.MySQL.Password,
			Database: appCfg.Authentication.MySQL.Database,
			Params:   appCfg.Authentication.MySQL.Params,
		}
		st, err = store.NewMySQLStore(mysqlCfg)
		if err != nil {
			log.Fatalf("init mysql store: %v", err)
		}
		if err := seedAdminUser(st); err != nil {
			log.Fatalf("seed admin user: %v", err)
		}
	}

	authSvc := auth.NewService(appCfg, st)

	feedbackPath := filepath.Join(baseDir, "logs", "feedback.log")
	updateAssetsDir := filepath.Dir(updateConfigPath)

	srv, err := server.New(appCfg, launcherCfg, maintenanceCfg, maintenanceConfigPath, newsCfg, newsConfigPath, updateCfg, updateConfigPath, updateAssetsDir, localizer, authSvc, st, feedbackPath)
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

func seedAdminUser(st store.Store) error {
	if st == nil {
		return nil
	}
	ctx := context.Background()
	has, err := st.HasUsers(ctx)
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	password, err := randomPassword()
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := st.CreateUser(ctx, "admin", string(hash), "admin"); err != nil {
		return err
	}
	log.Printf("created default admin user: username=admin password=%s (please change immediately)", password)
	return nil
}

func randomPassword() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("adm-%x", b), nil
}
