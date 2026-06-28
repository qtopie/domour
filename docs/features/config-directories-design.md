# Config Directories for Local MCP and Skills

This document details the design for introducing modular configuration directories for local MCP servers and Agent skills in Domour.

---

## 1. Motivation
Currently, MCP servers are declared in a single `config.json` under `mcp_servers`, and skills are loaded from a few hardcoded directory paths. 
To support modular configuration (e.g., dropping a JSON or Markdown file into a folder to add an MCP server or a new skill):
1. We introduce two directory configuration fields in `DomourConfig`: `mcp_dir` and `skills_dir`.
2. Any JSON file in `mcp_dir` (e.g. `mcp_dir/git.json`) is parsed as an individual `MCPServerConfig` and loaded.
3. Any JSON or Markdown file in `skills_dir` is parsed and loaded into the skill registry.

---

## 2. Configuration Schema Changes

We extend `DomourConfig` in `internal/config/config.go` with two fields:

```go
type DomourConfig struct {
	...
	MCPDir     string                     `json:"mcp_dir,omitempty"`
	SkillsDir  string                     `json:"skills_dir,omitempty"`
	MCPServers map[string]MCPServerConfig `json:"mcp_servers,omitempty"`
	...
}
```

- **Default Paths**:
  - `mcp_dir` defaults to `~/.domour/mcp`
  - `skills_dir` defaults to `~/.domour/skills` (or the existing default `skills` directory)

---

## 3. Loader Mechanisms

### A. Modular MCP Loader
In `internal/bionic/tool/mcp_loader.go`:
1. Resolve the `mcp_dir` path.
2. If the directory exists:
   - Walk the directory to find `.json` files.
   - For each JSON file (e.g., `git.json`), parse its content into an `MCPServerConfig`.
   - Add it to the list of servers to initialize.
3. Proceed with initialization and tool registration.

### B. Modular Skills Loader
In `internal/bionic/tool/skills.go` (and the `FileRegistry`):
1. Instantiate the `FileRegistry` pointing to the resolved `skills_dir`.
2. List and load all Markdown/JSON skills from that folder dynamically.
3. Integrate this into the `Manager`'s default skill loading lifecycle.
