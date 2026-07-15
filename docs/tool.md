支持的工具调用

* 本地注册(Internal Go tool)
* CLI
* gRPC
* MCP

当前实现约束：

* 所有 tool 统一由 `internal/pkg/motor` 的 manager 注册、懒加载、执行与空闲卸载。
* skill 也统一由 `internal/pkg/motor` 的 manager 管理，按 `skills/` 目录中的 Markdown 文件注册，并在首次使用时解析加载。
* provider 指令文件也可作为 skill 源统一读取，当前默认支持：
  * Gemini：`GEMINI.md`
  * Claude Code：`CLAUDE.md`、`CLAUDE.local.md`、`.claude/rules/*.md`
  * GitHub Copilot：`AGENTS.md`、`.github/copilot-instructions.md`、`.github/instructions/**/*.instructions.md`
  * Qoder：`QODER.md`、`.qoder/**/*.md`
* `motor` 只保存 tool manifest 与 loader，不要求所有 tool 在进程启动时常驻。
* 本地内建工具默认包括：
  * `shell.exec`：CLI tool
  * `search.grep`：Internal Go tool (powered by sniphunt)
  * `file.edit_lines`：Internal Go tool (line-range replacement)
  * `file.replace`：Internal Go tool (exact string replacement)
* gRPC / MCP tool 通过同一套 registry 接入，由调用方提供 client factory，生命周期仍由 `motor` 托管。
* skill 负责提供 instructions 与允许的工具清单，tool 继续负责真实执行；两者分离但共用一套生命周期管理。
