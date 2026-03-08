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

.PHONY: all build-plugins gen clean install

# The default target that builds everything
all: build-plugins

# Build all plugins defined in the PLUGINS variable
build-plugins: $(PLUGINS)

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