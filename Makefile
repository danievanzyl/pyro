REMOTE_HOST := root@homelab.local
REMOTE_DIR := /opt/pyro

.PHONY: build build-server build-agent build-cli test test-unit clean deploy deploy-bin deploy-agent deploy-service deploy-rootfs test-integration ssh

# Build all binaries (UI must be built before server for embed)
build: build-ui build-server build-agent build-cli

build-ui:
	cd ui && bun run build

build-server: build-ui
	go build -o bin/pyro-server ./cmd/server

# Agent must be cross-compiled for Linux (runs inside Firecracker VM)
build-agent:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/pyro-agent ./cmd/agent

build-cli:
	go build -o bin/pyro ./cmd/pyro

# Unit tests (run on any platform, no Firecracker needed)
test-unit:
	go test -v -short ./internal/...

# Full tests (requires Linux + KVM + Firecracker)
test:
	go test -v ./...

# Cross-compile everything for Linux (for deployment to KVM host)
build-linux: build-ui
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/linux/pyro-server ./cmd/server
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/linux/pyro-agent ./cmd/agent
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/linux/pyro ./cmd/pyro

# Deploy all binaries to KVM host
# TODO: remove ssh targets before publishing to github public
deploy: build-linux
	ssh $(REMOTE_HOST) 'mkdir -p $(REMOTE_DIR)/bin $(REMOTE_DIR)/images'
	scp bin/linux/pyro-server $(REMOTE_HOST):$(REMOTE_DIR)/bin/
	scp bin/linux/pyro-agent $(REMOTE_HOST):$(REMOTE_DIR)/bin/
	scp bin/linux/pyro $(REMOTE_HOST):$(REMOTE_DIR)/bin/

# Deploy just the server binary (fast iteration)
deploy-bin: build-linux
	scp bin/linux/pyro-server $(REMOTE_HOST):$(REMOTE_DIR)/bin/
	ssh $(REMOTE_HOST) 'systemctl restart pyro 2>/dev/null || true'

# Deploy just the agent binary
deploy-agent: build-linux
	scp bin/linux/pyro-agent $(REMOTE_HOST):$(REMOTE_DIR)/bin/

# Install systemd service + setup bridge
deploy-service:
	scp deploy/pyro.service $(REMOTE_HOST):/etc/systemd/system/
	scp deploy/setup-bridge.sh $(REMOTE_HOST):$(REMOTE_DIR)/
	ssh $(REMOTE_HOST) 'chmod +x $(REMOTE_DIR)/setup-bridge.sh && $(REMOTE_DIR)/setup-bridge.sh && systemctl daemon-reload && systemctl enable pyro'

# Inject latest agent into all rootfs images on host
deploy-rootfs: build-linux
	scp bin/linux/pyro-agent $(REMOTE_HOST):/tmp/pyro-agent-new
	ssh $(REMOTE_HOST) 'for img in $$(ls -d $(REMOTE_DIR)/images/*/rootfs.ext4 2>/dev/null); do \
		MNTDIR=$$(mktemp -d) && \
		mount -o loop $$img $$MNTDIR && \
		cp /tmp/pyro-agent-new $$MNTDIR/usr/bin/pyro-agent && \
		chmod 755 $$MNTDIR/usr/bin/pyro-agent && \
		umount $$MNTDIR && rmdir $$MNTDIR && \
		echo "updated: $$img"; \
	done && rm /tmp/pyro-agent-new'

# Run integration tests on remote KVM host
test-integration: build-linux
	scp bin/linux/pyro-server $(REMOTE_HOST):/tmp/
	scp bin/linux/pyro-agent $(REMOTE_HOST):/tmp/
	ssh $(REMOTE_HOST) 'cd /tmp && ./pyro-server --help'

# SSH into KVM host
ssh:
	ssh $(REMOTE_HOST)

clean:
	rm -rf bin/
