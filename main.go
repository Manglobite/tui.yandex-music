package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"tui.yamusic/internal/audio"
	"tui.yamusic/internal/config"
	"tui.yamusic/internal/logfile"
	"tui.yamusic/internal/music"
	"tui.yamusic/internal/tui"
)

func run() error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("не удалось определить текущий каталог")
	}
	cfg, err := config.Load(filepath.Join(root, ".env"))
	if err != nil {
		return err
	}
	var logger *logfile.Log
	if cfg.LogsEnabled {
		logDir := filepath.Join(root, "logs")
		if err := os.MkdirAll(logDir, 0700); err != nil {
			return fmt.Errorf("не удалось создать каталог logs: %w", err)
		}
		logger, err = logfile.Open(logDir)
		if err != nil {
			return err
		}
		defer logger.Close()
		logger.Record("start", 0, "", "", nil, 0)
		defer logger.Record("exit", 0, "", "", nil, 0)
	}
	demo := flag.Bool("demo", false, "offline design demonstration")
	preview := flag.Bool("preview", false, "print static design preview")
	width := flag.Int("width", 120, "preview width")
	height := flag.Int("height", 32, "preview height")
	mpv := flag.String("mpv", "mpv", "mpv executable")
	flag.Parse()
	if *demo || *preview {
		m := tui.NewDemo(*width, *height)
		if *preview {
			fmt.Println(m.View().Content)
			return nil
		}
		_, err := tea.NewProgram(m).Run()
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	player := audio.New(*mpv, filepath.Join(root, ".local", "runtime"), logger)
	defer player.Close()
	client := music.New(music.Credentials{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, StreamKey: cfg.StreamKey})
	m, err := tui.NewLive(ctx, client, player, logger, filepath.Join(root, ".local", "credentials", "token.enc"), cfg.SpectrumEnabled)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m).Run()
	cancel()
	return err
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
