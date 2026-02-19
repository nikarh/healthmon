package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DBPath               string
	DockerHost           string
	HTTPAddr             string
	TelegramEnabled      bool
	TelegramToken        string
	TelegramChatID       string
	RestartWindowSeconds int
	RestartThreshold     int
	WSOriginPatterns     []string
	WSInsecureSkipVerify bool
}

func Load() Config {
	origins := parseCSV(getEnv("HM_WS_ORIGINS", ""))
	if len(origins) == 0 {
		origins = defaultWSOriginPatterns()
	}
	return Config{
		DBPath:               getEnv("HM_DB_PATH", "./healthmon.db"),
		DockerHost:           getEnv("HM_DOCKER_HOST", "unix:///var/run/docker.sock"),
		HTTPAddr:             getEnv("HM_HTTP_ADDR", ":8080"),
		TelegramEnabled:      getEnvBool("HM_TG_ENABLED", false),
		TelegramToken:        os.Getenv("HM_TG_TOKEN"),
		TelegramChatID:       os.Getenv("HM_TG_CHAT_ID"),
		RestartWindowSeconds: getEnvInt("HM_RESTART_WINDOW_SECONDS", 300),
		RestartThreshold:     getEnvInt("HM_RESTART_THRESHOLD", 3),
		WSOriginPatterns:     origins,
		WSInsecureSkipVerify: getEnvBool("HM_WS_INSECURE_SKIP_VERIFY", false),
	}
}

func getEnv(key, def string) string {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	return val
}

func getEnvInt(key string, def int) int {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return def
	}
	return i
}

func getEnvBool(key string, def bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	switch strings.ToLower(val) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func parseCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
