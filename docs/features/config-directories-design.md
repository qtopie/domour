# Config Directories for Local MCP and Skills

This document details the design for introducing modular configuration directories for local MCP servers, Agent skills, and default prompt file hierarchy in Domour.

---

## 1. Directory Structure and Precedence

### User-Level Configuration (`~/.domour/`)
- **MCP Config File**: `~/.domour/mcp_config.json`
- **Skills Directory**: `~/.domour/skills/`

### Project-Level Configuration (`.agents/` in workspace root)
- **MCP Config File**: `.agents/mcp_config.json`
- **Skills Directory**: `.agents/skills/`

### Precedence Rules:
1. **MCP Configuration**: 
   - Load `~/.domour/mcp_config.json` (user-level defaults).
   - Load `.agents/mcp_config.json` (project-level overrides/additions).
   - Any server defined in project config overrides the server definition from user config.

2. **Skills Directory**:
   - Discover skills from `~/.domour/skills/` (user-level skills).
   - Discover skills from `.agents/skills/` (project-level skills).
   - Project-level skills override user-level skills if there are name collisions.

---

## 2. Default System/Instruction Prompt Priority

When discovering project-level prompt / instruction files, `AGENTS.md` is given top priority:

1. `AGENTS.md` (Workspace root)
2. `.agents/AGENTS.md`
3. `GEMINI.md` / `CLAUDE.md` / `QODER.md` / `copilot-instructions.md` (and legacy tool-specific paths)

---

## 3. Loader Mechanisms

### A. MCP Loader
In `internal/bionic/tool/mcp_loader.go`:
1. Load `~/.domour/mcp_config.json` (or `mcp_servers` section within it).
2. Load project-level `.agents/mcp_config.json` if present in current workspace/CWD.
3. Merge MCP server definitions into the active MCP client registry.

### B. Skills Loader
In `internal/bionic/skillmgr/skillmgr.go`:
1. Scan `~/.domour/skills/` for `.md` / `.json` skill specifications.
2. Scan `.agents/skills/` for project-specific `.md` / `.json` skill specifications.
3. Priority ordering ensures `AGENTS.md` instructions take precedence as default instruction skills over legacy files.

