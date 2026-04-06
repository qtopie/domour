package agent

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	appconfig "github.com/qtopie/domour/internal/app/config"
)

func newConfiguredBrainClient() (BrainClient, error) {
	cfg, err := appconfig.LoadDomourConfig()
	if err != nil {
		return nil, err
	}

	mode := strings.ToLower(firstNonEmpty(
		strings.TrimSpace(os.Getenv("DOMOUR_BRAIN_MODE")),
		cfg.ServiceMode("brain"),
	))
	if mode == "" || mode == "local" {
		return newLocalBrainClient()
	}
	if mode == "dapr" && !daprReachable(cfg.DaprGRPCAddress()) {
		return newLocalBrainClient()
	}
	if mode == "dapr" {
		return newDaprBrainClient(cfg)
	}
	return nil, fmt.Errorf("unsupported brain mode %q", mode)
}

func newConfiguredMotorClient() (MotorClient, error) {
	cfg, err := appconfig.LoadDomourConfig()
	if err != nil {
		return nil, err
	}

	mode := strings.ToLower(firstNonEmpty(
		strings.TrimSpace(os.Getenv("DOMOUR_MOTOR_MODE")),
		cfg.ServiceMode("motor"),
	))
	if mode == "" || mode == "local" {
		return newLocalMotorClient()
	}
	if mode == "dapr" && !daprReachable(cfg.DaprGRPCAddress()) {
		return newLocalMotorClient()
	}
	if mode == "dapr" {
		return nil, fmt.Errorf("dapr motor client is not implemented yet; use local motor with dapr brain for the first migration stage")
	}
	return nil, fmt.Errorf("unsupported motor mode %q", mode)
}

func daprReachable(address string) bool {
	address = strings.TrimSpace(address)
	if address == "" {
		return false
	}

	conn, err := net.DialTimeout("tcp", address, 800*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
