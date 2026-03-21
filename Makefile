REMOTE_HOST := root@homelab.local
REMOTE_DIR := /opt/firecrackerlacker

.PHONY: build build-server build-agent build-fcctl test test-unit clean deploy deploy-bin deploy-agent deploy-service deploy-rootfs test-integration ssh

# Build all binaries (UI must be built before server for embed)
build: build-ui build-server build-agent build-fcctl

build-ui:
	cd ui && bun run build

build-server: build-ui
	go build -o bin/firecrackerlacker-server ./cmd/server

# Agent must be cross-compiled for Linux (runs inside Firecracker VM)
build-agent:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/fc-agent ./cmd/agent

build-fcctl:
	go build -o bin/fcctl ./cmd/fcctl

# Unit tests (run on any platform, no Firecracker needed)
test-unit:
	go test -v -short ./internal/...

# Full tests (requires Linux + KVM + Firecracker)
test:
	go test -v ./...

# Cross-compile everything for Linux (for deployment to KVM host)
build-linux: build-ui
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/linux/firecrackerlacker-server ./cmd/server
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/linux/fc-agent ./cmd/agent
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/linux/fcctl ./cmd/fcctl

# Deploy all binaries to KVM host
# TODO: remove ssh targets before publishing to github public
deploy: build-linux
	ssh $(REMOTE_HOST) 'mkdir -p $(REMOTE_DIR)/bin $(REMOTE_DIR)/images'
	scp bin/linux/firecrackerlacker-server $(REMOTE_HOST):$(REMOTE_DIR)/bin/
	scp bin/linux/fc-agent $(REMOTE_HOST):$(REMOTE_DIR)/bin/
	scp bin/linux/fcctl $(REMOTE_HOST):$(REMOTE_DIR)/bin/

# Deploy just the server binary (fast iteration)
deploy-bin: build-linux
	scp bin/linux/firecrackerlacker-server $(REMOTE_HOST):$(REMOTE_DIR)/bin/
	ssh $(REMOTE_HOST) 'systemctl restart firecrackerlacker 2>/dev/null || true'

# Deploy just the agent binary
deploy-agent: build-linux
	scp bin/linux/fc-agent $(REMOTE_HOST):$(REMOTE_DIR)/bin/

# Install systemd service + setup bridge
deploy-service:
	scp deploy/firecrackerlacker.service $(REMOTE_HOST):/etc/systemd/system/
	scp deploy/setup-bridge.sh $(REMOTE_HOST):$(REMOTE_DIR)/
	ssh $(REMOTE_HOST) 'chmod +x $(REMOTE_DIR)/setup-bridge.sh && $(REMOTE_DIR)/setup-bridge.sh && systemctl daemon-reload && systemctl enable firecrackerlacker'

# Rebuild rootfs with latest agent and inject into host
deploy-rootfs: build-linux
	scp bin/linux/fc-agent $(REMOTE_HOST):/tmp/fc-agent-new
	ssh $(REMOTE_HOST) 'MNTDIR=$$(mktemp -d) && mount -o loop $(REMOTE_DIR)/images/default/rootfs.ext4 $$MNTDIR && cp /tmp/fc-agent-new $$MNTDIR/usr/bin/fc-agent && chmod 755 $$MNTDIR/usr/bin/fc-agent && umount $$MNTDIR && rmdir $$MNTDIR && rm /tmp/fc-agent-new && echo "rootfs updated"'

# Run integration tests on remote KVM host
test-integration: build-linux
	scp bin/linux/firecrackerlacker-server $(REMOTE_HOST):/tmp/
	scp bin/linux/fc-agent $(REMOTE_HOST):/tmp/
	ssh $(REMOTE_HOST) 'cd /tmp && ./firecrackerlacker-server --help'

# SSH into KVM host
ssh:
	ssh $(REMOTE_HOST)

clean:
	rm -rf bin/
