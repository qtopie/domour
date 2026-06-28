# Home Directory Override Design

This document details the design for resolving the default configuration home directory dynamically, allowing `cosmos-star` to override it with `~/.cosmos-star/` or custom environment variables.

---

## 1. Motivation
Domour defaults its configurations, MCP configs, and skills under `~/.domour/`. However, when integrated into `cosmos-star`, the default configuration home should be `~/.cosmos-star/` (or paths set by the host environment). 

We need a flexible, environment-aware mechanism to determine the configuration root directory.

---

## 2. Dynamic Home Path Resolution

We define a helper function `DomourHomeDir()` in `internal/config/config.go` that resolves the home directory path in order of precedence:

1. **Environment Variables**:
   - `DOMOUR_HOME` (explicit override for Domour home)
   - `COSMOS_STAR_HOME` (explicit override for cosmos-star)
   - `COSMOS_HOME` (legacy or shared override)
2. **Directory Probing**:
   - If `~/.cosmos-star` exists, use it.
   - If `~/.cosmos` exists, use it.
3. **Default**:
   - Falls back to `~/.domour`

---

## 3. Configuration Path Integration

All default path constructors and normalizers will use `DomourHomeDir()` instead of hardcoded `~/.domour` or `os.UserHomeDir()` references:

```go
func DomourConfigPath() (string, error) {
	return filepath.Join(DomourHomeDir(), "config.json"), nil
}
```

And in `normalizeDomourConfig`:
```go
	home := DomourHomeDir()
	cfg.MCPDir = strings.TrimSpace(cfg.MCPDir)
	if cfg.MCPDir == "" {
		cfg.MCPDir = filepath.Join(home, "mcp")
	}
	cfg.SkillsDir = strings.TrimSpace(cfg.SkillsDir)
	if cfg.SkillsDir == "" {
		cfg.SkillsDir = filepath.Join(home, "skills")
	}
```
This ensures consistent path resolution across all files and platforms.
