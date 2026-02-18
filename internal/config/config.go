package config

import (
	"os"
	"strconv"
)

type Config struct {
	DBPath               string
	DockerSocket         string
	HTTPAddr             string
	TelegramToken        string
	TelegramChatID       string
	RestartWindowSeconds int
	RestartThreshold     int
	EventCacheLimit      int
}

func Load() Config {
	return Config{
		DBPath:               getEnv("HM_DB_PATH", "./healthmon.db"),
		DockerSocket:         getEnv("HM_DOCKER_SOCKET", "/var/run/docker.sock"),
		HTTPAddr:             getEnv("HM_HTTP_ADDR", ":8080"),
		TelegramToken:        os.Getenv("HM_TG_TOKEN"),
		TelegramChatID:       os.Getenv("HM_TG_CHAT_ID"),
		RestartWindowSeconds: getEnvInt("HM_RESTART_WINDOW_SECONDS", 300),
		RestartThreshold:     getEnvInt("HM_RESTART_THRESHOLD", 3),
		EventCacheLimit:      getEnvInt("HM_EVENT_CACHE_LIMIT", 5000),
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
