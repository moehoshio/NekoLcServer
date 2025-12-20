package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
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

	// Initialize store based on database configuration
	st, err := initStore(appCfg)
	if err != nil {
		log.Fatalf("init store: %v", err)
	}

	if st != nil {
		if err := seedAdminUser(st); err != nil {
			log.Fatalf("seed admin user: %v", err)
		}
	}

	// Try to load configurations from database first, fall back to JSON files
	languageBundle, err := loadLanguagesConfig(st, resolve(appCfg.Language.ConfigPath))
	if err != nil {
		log.Fatalf("load languages: %v", err)
	}
	localizer := localization.New(appCfg.Language.Default, languageBundle)

	launcherCfg, err := loadLauncherConfig(st, resolve(appCfg.Launcher.ConfigPath))
	if err != nil {
		log.Fatalf("load launcher config: %v", err)
	}

	maintenanceConfigPath := resolve(appCfg.Maintenance.ConfigPath)
	maintenanceCfg, err := loadMaintenanceConfig(st, maintenanceConfigPath)
	if err != nil {
		log.Fatalf("load maintenance config: %v", err)
	}

	newsConfigPath := resolve(appCfg.News.ConfigPath)
	newsCfg, err := loadNewsConfig(st, newsConfigPath)
	if err != nil {
		log.Fatalf("load news config: %v", err)
	}

	updateConfigPath := resolve(appCfg.Update.ConfigPath)
	updateCfg, err := loadUpdatesConfig(st, updateConfigPath)
	if err != nil {
		log.Fatalf("load update config: %v", err)
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

func initStore(appCfg *config.AppConfig) (store.Store, error) {
	dbType := strings.ToLower(strings.TrimSpace(appCfg.Database.Type))

	// For backward compatibility, check authentication method if database type not set
	if dbType == "" {
		if strings.EqualFold(appCfg.Authentication.Method, "mysql") {
			dbType = "mysql"
		}
	}

	switch dbType {
	case "mysql":
		// Check new database config first, fall back to authentication config
		mysqlCfg := store.MySQLConfig{
			Host:     appCfg.Database.MySQL.Host,
			Port:     appCfg.Database.MySQL.Port,
			Username: appCfg.Database.MySQL.Username,
			Password: appCfg.Database.MySQL.Password,
			Database: appCfg.Database.MySQL.Database,
			Params:   appCfg.Database.MySQL.Params,
		}
		// Fall back to authentication config for backward compatibility
		if mysqlCfg.Host == "" {
			mysqlCfg.Host = appCfg.Authentication.MySQL.Host
			mysqlCfg.Port = appCfg.Authentication.MySQL.Port
			mysqlCfg.Username = appCfg.Authentication.MySQL.Username
			mysqlCfg.Password = appCfg.Authentication.MySQL.Password
			mysqlCfg.Database = appCfg.Authentication.MySQL.Database
			mysqlCfg.Params = appCfg.Authentication.MySQL.Params
		}
		return store.NewMySQLStore(mysqlCfg)

	case "sqlite":
		sqliteCfg := store.SQLiteConfig{
			Path: appCfg.Database.SQLite.Path,
		}
		if sqliteCfg.Path == "" {
			sqliteCfg.Path = "nekoserver.db"
		}
		return store.NewSQLiteStore(sqliteCfg)

	default:
		// Use in-memory store for memory mode or when not specified
		return store.NewMemory(), nil
	}
}

func loadLanguagesConfig(st store.Store, filePath string) (config.LanguageBundle, error) {
	if st != nil {
		ctx := context.Background()
		data, err := st.GetConfig(ctx, store.ConfigKeyLanguages)
		if err == nil {
			var bundle config.LanguageBundle
			if err := json.Unmarshal(data, &bundle); err == nil {
				log.Printf("Loaded languages config from database")
				return bundle, nil
			}
		}
	}
	// Fall back to file
	return config.LoadLanguages(filePath)
}

func loadLauncherConfig(st store.Store, filePath string) (*config.LauncherConfig, error) {
	if st != nil {
		ctx := context.Background()
		data, err := st.GetConfig(ctx, store.ConfigKeyLauncher)
		if err == nil {
			var cfg config.LauncherConfig
			if err := json.Unmarshal(data, &cfg); err == nil {
				log.Printf("Loaded launcher config from database")
				return &cfg, nil
			}
		}
	}
	// Fall back to file
	return config.LoadLauncher(filePath)
}

func loadMaintenanceConfig(st store.Store, filePath string) (*config.MaintenanceConfig, error) {
	if st != nil {
		ctx := context.Background()
		data, err := st.GetConfig(ctx, store.ConfigKeyMaintenance)
		if err == nil {
			var cfg config.MaintenanceConfig
			if err := json.Unmarshal(data, &cfg); err == nil {
				log.Printf("Loaded maintenance config from database")
				return &cfg, nil
			}
		}
	}
	// Fall back to file
	return config.LoadMaintenance(filePath)
}

func loadNewsConfig(st store.Store, filePath string) (*config.NewsConfig, error) {
	if st != nil {
		ctx := context.Background()
		data, err := st.GetConfig(ctx, store.ConfigKeyNews)
		if err == nil {
			var cfg config.NewsConfig
			if err := json.Unmarshal(data, &cfg); err == nil {
				log.Printf("Loaded news config from database")
				return &cfg, nil
			}
		}
	}
	// Fall back to file
	return config.LoadNews(filePath)
}

func loadUpdatesConfig(st store.Store, filePath string) (*config.UpdateConfig, error) {
	if st != nil {
		ctx := context.Background()
		data, err := st.GetConfig(ctx, store.ConfigKeyUpdates)
		if err == nil {
			var cfg config.UpdateConfig
			if err := json.Unmarshal(data, &cfg); err == nil {
				log.Printf("Loaded updates config from database")
				return &cfg, nil
			}
		}
	}
	// Fall back to file
	return config.LoadUpdates(filePath)
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
	log.Printf("============================================================")
	log.Printf("IMPORTANT: Default admin account created!")
	log.Printf("Username: admin")
	log.Printf("Password: %s", password)
	log.Printf("Please change this password immediately after first login!")
	log.Printf("============================================================")
	return nil
}

func randomPassword() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("adm-%x", b), nil
}
