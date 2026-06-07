package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/qtopie/domour/ark/bootstrap"
	"github.com/qtopie/domour/internal/app/assistant"
	"github.com/qtopie/domour/internal/bionic/session"
	"github.com/qtopie/domour/internal/config/modelmanager"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "models" {
		if err := runModelsCommand(os.Args[2:]); err != nil {
			log.Fatalf("domour models failed: %v", err)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "sessions" {
		if err := runSessionsCommand(os.Args[2:]); err != nil {
			log.Fatalf("domour sessions failed: %v", err)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := bootstrap.Run(ctx); err != nil {
		log.Fatalf("domour server exited: %v", err)
	}
}

func runModelsCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected subcommand: list or set")
	}

	switch args[0] {
	case "list":
		return runModelsList(args[1:])
	case "set":
		return runModelsSet(args[1:])
	default:
		return fmt.Errorf("unsupported models subcommand %q", args[0])
	}
}

func runModelsList(args []string) error {
	fs := flag.NewFlagSet("models list", flag.ContinueOnError)
	provider := fs.String("provider", "", "provider to inspect (defaults to resolved entry/default provider)")
	entry := fs.String("entry", "", "entry to resolve against: chat, copilot, autopilot, or default")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	timeout := fs.Duration("timeout", 15*time.Second, "discovery timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	resp, err := modelmanager.Discover(ctx, modelmanager.DiscoverRequest{
		Entry:    *entry,
		Provider: *provider,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(resp)
	}

	fmt.Printf("provider: %s\n", resp.Provider)
	if resp.Entry != "" {
		fmt.Printf("entry: %s\n", resp.Entry)
	} else {
		fmt.Println("entry: default")
	}
	if resp.SelectedModel != "" {
		fmt.Printf("selected model: %s\n", resp.SelectedModel)
	}
	fmt.Printf("discovery supported: %t\n", resp.DiscoverySupported)
	if resp.Cached {
		fmt.Println("cached: true")
	}
	if resp.Source != "" {
		fmt.Printf("source: %s\n", resp.Source)
	}
	if resp.Message != "" {
		fmt.Printf("note: %s\n", resp.Message)
	}
	fmt.Printf("config: %s\n", resp.ConfigPath)
	fmt.Println("models:")
	for _, model := range resp.Models {
		fmt.Printf("- %s\n", model)
	}
	if len(resp.Models) == 0 {
		fmt.Println("- (none detected)")
	}
	return nil
}

func runModelsSet(args []string) error {
	fs := flag.NewFlagSet("models set", flag.ContinueOnError)
	provider := fs.String("provider", "", "provider to configure (optional if entry/default already has one)")
	model := fs.String("model", "", "model identifier to persist")
	entry := fs.String("entry", "", "entry to configure: chat, copilot, autopilot, or default")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resp, err := modelmanager.SetModel(context.Background(), modelmanager.SetModelRequest{
		Entry:    *entry,
		Provider: *provider,
		Model:    *model,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(resp)
	}

	if resp.Entry != "" {
		fmt.Printf("entry: %s\n", resp.Entry)
	} else {
		fmt.Println("entry: default")
	}
	fmt.Printf("provider: %s\n", resp.Provider)
	fmt.Printf("model: %s\n", resp.Model)
	fmt.Printf("config: %s\n", resp.ConfigPath)
	return nil
}

func runSessionsCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected subcommand: list")
	}

	switch args[0] {
	case "list":
		return runSessionsList(args[1:])
	default:
		return fmt.Errorf("unsupported sessions subcommand %q", args[0])
	}
}

func runSessionsList(args []string) error {
	fs := flag.NewFlagSet("sessions list", flag.ContinueOnError)
	provider := fs.String("provider", "", "filter sessions by LLM provider")
	sessionID := fs.String("session", "", "filter sessions by session ID")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	store := assistant.InitStore(nil)
	if store != nil {
		defer store.Close()
	}

	results, err := session.QuerySessions(context.Background(), store, session.QueryFilter{
		Provider:  *provider,
		SessionID: *sessionID,
	})
	if err != nil {
		return err
	}

	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	fmt.Printf("%-38s | %-12s | %-22s | %-19s | %s\n", "SESSION ID", "PROVIDER", "MODEL", "UPDATED AT", "LAST MESSAGE")
	fmt.Println(strings.Repeat("-", 120))
	for _, res := range results {
		lastMsg := res.LastMessage
		if len(lastMsg) > 40 {
			lastMsg = lastMsg[:37] + "..."
		}
		lastMsg = strings.ReplaceAll(lastMsg, "\n", " ")

		fmt.Printf("%-38s | %-12s | %-22s | %-19s | %s\n",
			res.SessionID,
			res.Provider,
			res.Model,
			res.UpdatedAt.Format("2006-01-02 15:04:05"),
			lastMsg,
		)
	}
	return nil
}
