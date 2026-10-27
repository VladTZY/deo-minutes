package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"deo-minutes/internal/bot"
	"deo-minutes/internal/config"
)

// runSelftest implements `DEOMinute.exe --selftest <link>` (PLAN.md S1): join
// a real meeting, record a few minutes and save screenshots, audio and the
// roster to a local folder for tuning the Webex selectors. The meeting
// password, if not in the link, comes from the DEO_MEETING_PASSWORD
// environment variable so it does not show up in the process list.
func runSelftest(args []string) int {
	fs := flag.NewFlagSet("selftest", flag.ContinueOnError)
	minutes := fs.Int("minutes", 3, "cât timp rămâne botul în ședință")
	visible := fs.Bool("visible", false, "arată fereastra browserului")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "utilizare: DEOMinute.exe --selftest [--minutes N] [--visible] <link Webex>")
		return 2
	}
	link, err := bot.ParseLink(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		cfg = config.Defaults()
	}
	dir := filepath.Join(config.AppDataDir(), "selftest", time.Now().Format("20060102-150405"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*minutes+20)*time.Minute)
	defer cancel()
	_, err = bot.Selftest(ctx, bot.Options{
		Link:         link,
		Password:     os.Getenv("DEO_MEETING_PASSWORD"),
		DisplayName:  cfg.Bot.DisplayName,
		Email:        cfg.Bot.Email,
		BrowserPath:  cfg.Bot.BrowserPath,
		Headless:     !*visible,
		LobbyTimeout: time.Duration(cfg.Bot.LobbyTimeoutMin) * time.Minute,
		Selectors:    bot.DefaultSelectors,
	}, dir, time.Duration(*minutes)*time.Minute)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	openFolder(dir)
	if err != nil {
		return 1
	}
	return 0
}

func openFolder(dir string) {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("explorer.exe", dir).Start()
	case "darwin":
		_ = exec.Command("open", dir).Start()
	}
}
