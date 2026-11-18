package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	ClientID     = "YAMUSIC_CLIENT_ID"
	ClientSecret = "YAMUSIC_CLIENT_SECRET"
	StreamKey    = "YAMUSIC_STREAM_KEY"
	Logs         = "YAMUSIC_LOGS"
	Spectrum     = "YAMUSIC_SPECTRUM"
)

type Config struct {
	ClientID, ClientSecret, StreamKey string
	LogsEnabled, SpectrumEnabled      bool
}

func Load(path string) (Config, error) {
	values := map[string]string{}
	f, err := os.Open(path)
	if err == nil {
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		for line := 1; scanner.Scan(); line++ {
			text := strings.TrimSpace(scanner.Text())
			if text == "" || strings.HasPrefix(text, "#") {
				continue
			}
			key, value, ok := strings.Cut(text, "=")
			if !ok || strings.TrimSpace(key) == "" {
				return Config{}, fmt.Errorf(".env: ожидалось ИМЯ=значение, строка %d", line)
			}
			key = strings.TrimSpace(key)
			if key == ClientID || key == ClientSecret || key == StreamKey || key == Logs || key == Spectrum {
				values[key] = strings.TrimSpace(value)
			}
		}
		if scanner.Err() != nil {
			return Config{}, errors.New("не удалось прочитать .env")
		}
	} else if !os.IsNotExist(err) {
		return Config{}, errors.New("не удалось открыть .env")
	}
	get := func(key string) string {
		if value, ok := os.LookupEnv(key); ok {
			return strings.TrimSpace(value)
		}
		return values[key]
	}
	cfg := Config{ClientID: get(ClientID), ClientSecret: get(ClientSecret), StreamKey: get(StreamKey)}
	cfg.SpectrumEnabled = true
	for _, option := range []struct {
		key string
		dst *bool
	}{{Logs, &cfg.LogsEnabled}, {Spectrum, &cfg.SpectrumEnabled}} {
		if value := get(option.key); value != "" {
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return Config{}, fmt.Errorf("%s: ожидается true или false", option.key)
			}
			*option.dst = enabled
		}
	}
	return cfg, nil
}
