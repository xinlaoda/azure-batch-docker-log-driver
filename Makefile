PLUGIN_NAME ?= azure-batch-docker-log-driver
PLUGIN_TAG ?= latest
PLUGIN_FULL ?= $(PLUGIN_NAME):$(PLUGIN_TAG)

.PHONY: all clean build rootfs create enable push test

all: clean build rootfs create enable

# Build the Go binary inside Docker
build:
	docker build -t $(PLUGIN_NAME)-build:$(PLUGIN_TAG) .

# Create rootfs from the built image
rootfs:
	@rm -rf plugin/rootfs
	@mkdir -p plugin/rootfs
	docker create --name $(PLUGIN_NAME)-tmp $(PLUGIN_NAME)-build:$(PLUGIN_TAG) true
	docker export $(PLUGIN_NAME)-tmp | tar -x -C plugin/rootfs
	docker rm -f $(PLUGIN_NAME)-tmp
	cp config.json plugin/

# Create the Docker managed plugin
create:
	docker plugin rm -f $(PLUGIN_FULL) 2>/dev/null || true
	docker plugin create $(PLUGIN_FULL) ./plugin

# Enable the plugin
enable:
	docker plugin enable $(PLUGIN_FULL)

# Push to ACR (requires: docker login myacr.azurecr.io)
push:
	docker plugin push $(PLUGIN_FULL)

# Run tests
test:
	go test -v -race ./...

# Clean up
clean:
	docker plugin rm -f $(PLUGIN_FULL) 2>/dev/null || true
	docker rm -f $(PLUGIN_NAME)-tmp 2>/dev/null || true
	docker rmi -f $(PLUGIN_NAME)-build:$(PLUGIN_TAG) 2>/dev/null || true
	rm -rf plugin/
