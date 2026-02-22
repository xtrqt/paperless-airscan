package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Scanner   ScannerConfig
	Paperless PaperlessConfig
	Server    ServerConfig
	TitlePage TitlePageConfig
	Storage   StorageConfig
}

type ScannerConfig struct {
	Host    string
	Timeout time.Duration
	Duplex  bool
	Source  string
	Color   string
	Reorder string
}

type PaperlessConfig struct {
	URL   string
	Token string
}

type ServerConfig struct {
	Addr string
}

type TitlePageConfig struct {
	Enabled   bool
	WeekStart string
}

type StorageConfig struct {
	DatabasePath string
	TempDir      string
}

func Load() *Config {
	return &Config{
		Scanner: ScannerConfig{
			Host:    getEnv("SCANNER_HOST", ""),
			Timeout: getDurationEnv("SCANNER_TIMEOUT", 30*time.Second),
			Duplex:  getBoolEnv("SCANNER_DUPLEX", true),
			Source:  getEnv("SCANNER_SOURCE", "adf"),
			Color:   getEnv("SCANNER_COLOR", "grayscale"),
			Reorder: getEnv("SCANNER_REORDER", ""),
		},
		Paperless: PaperlessConfig{
			URL:   os.Getenv("PAPERLESS_URL"),
			Token: os.Getenv("PAPERLESS_TOKEN"),
		},
		Server: ServerConfig{
			Addr: getEnv("SERVER_ADDR", ":8080"),
		},
		TitlePage: TitlePageConfig{
			Enabled:   getBoolEnv("TITLE_PAGE_ENABLED", true),
			WeekStart: getEnv("TITLE_PAGE_WEEK_START", "monday"),
		},
		Storage: StorageConfig{
			DatabasePath: getEnv("DATABASE_PATH", "/data/paperless-airscan.db"),
			TempDir:      getEnv("TEMP_DIR", "/tmp/paperless-airscan"),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getBoolEnv(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		b, err := strconv.ParseBool(value)
		if err != nil {
			return defaultValue
		}
		return b
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		d, err := time.ParseDuration(value)
		if err != nil {
			return defaultValue
		}
		return d
	}
	return defaultValue
}
