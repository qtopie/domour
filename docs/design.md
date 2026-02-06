
UI 交互、意图识别、多模态感知及任务编排

AI服务插件化(AI模型能力/SKILL/MCP/Function CALL)

插件化
* caddy module
* hashicorp go plugin(grpc)


方案,实现机制,跨平台兼容性,性能,插件开发难度,适合场景
1. 静态编译 (Caddy 模式),源码级 import，通过 init() 注册接口。,极佳。一份源码到处编译。,最高（原生调用）,低（纯 Go 开发）,核心功能插件（如加密协议、基础存储）。
2. HashiCorp go-plugin,主从进程模式，通过 gRPC 通信。,优秀。支持 Unix Domain Socket (L/M) 和 TCP (W)。,中（有跨进程通信开销）,中（需定义 Protobuf）,算力密集型插件（如 LLM 推理后端、复杂爬虫）。
3. WebAssembly (Wasm),嵌入 wazero 运行时加载 .wasm 字节码。,极佳。真正的一份二进制到处运行。,中下（解释执行/AOT）,较高（需编译为 Wasm）,安全敏感或第三方贡献的插件（如自定义处理脚本）。
4. Yaegi (动态解释器),在运行时直接解析执行 .go 源码。,优秀。无需额外环境。,较低（解释执行）,极低（直接写 Go 代码）,经常变动的办公自动化逻辑、小工具。



LLM
tools
blabla
