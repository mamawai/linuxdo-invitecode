package main

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Port       string
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	RedisAddr  string
	RedisPass  string
	RedisDB    int
	ResendKey  string
	ResendFrom string
	FrontURL   string
	AdminKey   string
	AltchaKey  string
}

func loadConfig() *Config {
	_ = godotenv.Load()

	cfg := &Config{
		Port:       getEnv("PORT", "7386"),
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "mawai"),
		DBPassword: getEnv("DB_PASSWORD", ""),
		DBName:     getEnv("DB_NAME", "wiib"),
		RedisAddr:  getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPass:  getEnv("REDIS_PASSWORD", ""),
		RedisDB:    getIntEnv("REDIS_DB", 0),
		ResendKey:  getEnv("RESEND_API_KEY", ""),
		ResendFrom: getEnv("RESEND_FROM_EMAIL", "noreply@mynnmy.top"),
		FrontURL:   getEnv("FRONT_URL", "http://localhost:7386"),
		AdminKey:   getEnv("ADMIN_UPLOAD_KEY", ""),
		AltchaKey:  getEnv("ALTCHA_HMAC_KEY", ""),
	}

	if cfg.DBPassword == "" {
		log.Fatal("DB_PASSWORD 未设置")
	}
	if cfg.AdminKey == "" {
		log.Fatal("ADMIN_UPLOAD_KEY 未设置")
	}
	if cfg.ResendKey == "" {
		log.Fatal("RESEND_API_KEY 未设置")
	}
	return cfg
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getIntEnv(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
