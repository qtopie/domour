# Define the Go compiler
GO := go

# Define the build flags for plugins
PLUGIN_FLAGS := -buildmode=plugin

# Define the plugin output names
PLUGINS := gemini.so mock.so eino.so

# Define the source paths for each plugin using target-specific variable names
PLUGIN_SRC_eino.so := cmd/plugins/copilot/eino/main.go
PLUGIN_SRC_gemini.so := cmd/plugins/copilot/gemini/main.go
PLUGIN_SRC_mock.so := cmd/plugins/copilot/mock/main.go

# Define the installation directory
INSTALL_DIR := /opt/homa/plugins/copilot

#------------------------------------------------------------------------------
# Targets
#------------------------------------------------------------------------------

.PHONY: all build-plugins gen clean install surrealdb nats chat-agent

# The default target that builds everything
all: build-plugins chat-agent

# Build all plugins defined in the PLUGINS variable
build-plugins: $(PLUGINS)

chat-agent: gen
	@echo "Building chat-agent..."
	$(GO) build -o chat-agent cmd/chat-agent/main.go

# A pattern rule to build each plugin. It depends on 'gen' to ensure
# code generation happens before the build.
%.so: gen
	@echo "Building plugin: $@"
	$(GO) build $(PLUGIN_FLAGS) -o $@ ${PLUGIN_SRC_$@}

# Target to run buf for code generation
gen:
	@command -v buf >/dev/null 2>&1 || { echo "Buf is not installed. Install it with 'brew install buf'."; exit 1; }
	buf generate

# Target to clean up generated files and plugins
clean:
	@echo "Cleaning generated files..."
	rm -rf gen $(PLUGINS)

# Target to install the plugins to the specified directory
install: build-plugins
	@echo "Installing plugins to $(INSTALL_DIR)..."
	@mkdir -p $(INSTALL_DIR)
	@cp $(PLUGINS) $(INSTALL_DIR)
	@echo "Installation complete."

# SurrealDB helper values
SURREAL_BIND := 127.0.0.1:8000
SURREAL_USER := root
SURREAL_PASS := root
SURREAL_ENGINE := memory

# Target to start a local SurrealDB instance for development/testing.
surrealdb:
	@command -v surreal >/dev/null 2>&1 || { echo "SurrealDB CLI is missing. Install it via 'brew install surrealdb' or https://surrealdb.com/download."; exit 1; }
	@echo "Starting SurrealDB on $(SURREAL_BIND) (press Ctrl+C to stop)..."
	surreal start \
		--user $(SURREAL_USER) \
		--pass $(SURREAL_PASS) \
		--bind $(SURREAL_BIND) \
		--log debug \
		$(SURREAL_ENGINE)

# Target to start a local NATS server with JetStream enabled for development/testing.
NATS_PORT := 4222
NATS_HTTP_PORT := 8222
NATS_STORE := /tmp/domour-nats

nats:
	@command -v nats-server >/dev/null 2>&1 || { echo "NATS Server is missing. Install it via 'brew install nats-server' or https://nats.io/download/."; exit 1; }
	@echo "Starting NATS Server with JetStream on 0.0.0.0:$(NATS_PORT) (press Ctrl+C to stop)..."
	@mkdir -p $(NATS_STORE)
	nats-server \
		-js \
		--port $(NATS_PORT) \
		--http_port $(NATS_HTTP_PORT) \
		--jetstream_store_dir $(NATS_STORE)
