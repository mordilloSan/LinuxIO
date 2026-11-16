# The disposable test VM (linuxio-e2e on qemu:///system) and the tests that run
# in it. packaging/vm/linuxio-vm.sh does the work; its state lives in
# ~/.cache/linuxio-vm, never in git. docs/development.md, "Test VM".

vm_script := $(repo_root)/packaging/vm/linuxio-vm.sh
# VM_RELEASE=noble selects the second VM (linuxio-e2e-noble, Ubuntu 24.04),
# kept for the root tests on an older kernel.
VM_RELEASE ?=
export LINUXIO_VM_RELEASE := $(VM_RELEASE)
vm_state_dir := $(HOME)/.cache/linuxio-vm$(if $(filter-out resolute,$(VM_RELEASE)),-$(VM_RELEASE))

##@ Test VM|yellow
vm-create: ## Create the test VM from the Ubuntu 26.04 cloud image and snapshot it as clean
	@"$(vm_script)" create

vm-install: build-nocheck ## Build LinuxIO and install it in the test VM
	@"$(vm_script)" install "$(bin_dir)"

vm-ssh: ## Run CMD='...' in the test VM over SSH (no CMD: a shell)
	@"$(vm_script)" ssh $${CMD:+"$$CMD"}

vm-reset: ## Revert the test VM to its clean snapshot and stop it
	@"$(vm_script)" reset

vm-destroy: ## Delete the test VM and its linuxio-e2e-* volumes
	@"$(vm_script)" destroy

vm-test-btrfs-integration: ensure-go ## Run the root btrfs and partition kernel tests in the test VM
	@set -e
	@mkdir -p "$(cache_dir)/vm"
	@cd "$(backend_dir)" && $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" CGO_ENABLED=0 "$(GO_BIN)" test -c -o "$(cache_dir)/vm/btrfs.test" ./common/btrfs
	@cd "$(backend_dir)" && $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" CGO_ENABLED=0 "$(GO_BIN)" test -c -o "$(cache_dir)/vm/storage.test" ./bridge/handlers/storage
	@"$(vm_script)" ssh 'cat > btrfs.test && chmod +x btrfs.test' < "$(cache_dir)/vm/btrfs.test"
	@"$(vm_script)" ssh 'cat > storage.test && chmod +x storage.test' < "$(cache_dir)/vm/storage.test"
	@"$(vm_script)" ssh "echo \"VM kernel: \$$(uname -r)\" && sudo env LINUXIO_RUN_BTRFS_INTEGRATION=1 ./btrfs.test -test.run $$(printf %q '$(BTRFS_INTEGRATION_RUN)') -test.count=1 -test.v -test.timeout=20m"
	@"$(vm_script)" ssh "sudo env LINUXIO_RUN_BTRFS_INTEGRATION=1 ./storage.test -test.run $$(printf %q '$(BTRFS_INTEGRATION_RUN)') -test.count=1 -test.v -test.timeout=20m"

# TestMemoryIntegration (backend/bridge/handlers/memory) switches the VM to
# zram and back through systemd and the kernel. It needs zram-tools and a swap
# file in fstab: the first run installs zram-tools (stopping the zramswap.service
# its package starts) and adds a 1 GiB /swap.img; make vm-reset undoes both.
# ZRAM_PROVIDER=zram-generator runs it with systemd's zram-generator instead
# (the Fedora and RHEL path, from Ubuntu's systemd-zram-generator package).
# LinuxIO uses the generator only when zram-tools is absent, so that run needs
# a VM without zram-tools: run make vm-reset first.
ZRAM_PROVIDER ?= zram-tools
ifeq ($(ZRAM_PROVIDER),zram-generator)
vm_memory_zram_setup := set -e; if test -x /usr/sbin/zramswap; then echo "zram-tools is installed and LinuxIO prefers it: run make vm-reset first" >&2; exit 1; fi; if ! test -x /usr/lib/systemd/system-generators/zram-generator; then sudo apt-get update -qq && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq systemd-zram-generator; fi; if grep -q "^/dev/zram" /proc/swaps; then sudo systemctl stop dev-zram0.swap; fi
else ifeq ($(ZRAM_PROVIDER),zram-tools)
vm_memory_zram_setup := set -e; if ! test -x /usr/sbin/zramswap; then sudo apt-get update -qq && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq zram-tools && sudo systemctl disable --now zramswap.service; fi
else
vm_memory_zram_setup := echo "ZRAM_PROVIDER must be zram-tools or zram-generator" >&2; exit 1
endif
# TestMemoryDrainIntegration parks data on the data disk vdb, used whole as
# swap. Its fstab entry is noauto, so it swaps only for the test: an active
# swap partition rules the zram mode out. pri=10 puts it ahead of /swap.img.
# A disk that holds anything but swap is another check's, and is refused.
# The test runs twice: with 2 GiB, then with 320 MiB and its reads from the
# disk capped at vm_memory_drain_rate bytes a second, where the swapoff has
# to outlast the 90 seconds systemd gives a swap unit.
vm_memory_drain_rate := 2097152
vm_memory_drain_env := LINUXIO_RUN_MEMORY_INTEGRATION=1 LINUXIO_MEMORY_DRAIN_DEVICE=/dev/vdb
vm_memory_drain_setup := set -e; kind=$$(sudo blkid -o value -s TYPE /dev/vdb || true); if test -n "$$kind" && test "$$kind" != swap; then echo "/dev/vdb holds $$kind, which another check put there: run make vm-reset first" >&2; exit 1; fi; if test -z "$$kind"; then sudo mkswap /dev/vdb >/dev/null; fi; if ! grep -qs "^/dev/vdb[[:space:]]" /etc/fstab; then echo "/dev/vdb none swap sw,noauto,pri=10 0 0" | sudo tee -a /etc/fstab >/dev/null && sudo systemctl daemon-reload; fi; sudo systemctl start dev-vdb.swap
vm-test-memory-integration: ensure-go ## Run the root swap mode and drain tests in the test VM
	@set -e
	@mkdir -p "$(cache_dir)/vm"
	@cd "$(backend_dir)" && $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" CGO_ENABLED=0 "$(GO_BIN)" test -c -o "$(cache_dir)/vm/memory.test" ./bridge/handlers/memory
	@"$(vm_script)" ssh '$(vm_memory_zram_setup)'
	@"$(vm_script)" ssh 'set -e; if ! grep -qs "^/swap.img[[:space:]]" /etc/fstab; then sudo fallocate -l 1G /swap.img && sudo chmod 600 /swap.img && sudo mkswap /swap.img >/dev/null && echo "/swap.img none swap sw 0 0" | sudo tee -a /etc/fstab >/dev/null && sudo systemctl daemon-reload && sudo systemctl start swap.img.swap; fi'
	@"$(vm_script)" ssh 'cat > memory.test && chmod +x memory.test' < "$(cache_dir)/vm/memory.test"
	@"$(vm_script)" ssh "echo \"VM kernel: \$$(uname -r)\" && sudo env LINUXIO_RUN_MEMORY_INTEGRATION=1 ./memory.test -test.run '^TestMemoryIntegration\$$' -test.count=1 -test.v -test.timeout=30m"
	@trap '"$(vm_script)" ssh "sudo systemctl stop dev-vdb.swap" || echo "/dev/vdb still swaps in the VM: run make vm-reset" >&2' EXIT
	@"$(vm_script)" ssh '$(vm_memory_drain_setup)'
	@"$(vm_script)" ssh "sudo env $(vm_memory_drain_env) LINUXIO_MEMORY_DRAIN_MIB=2048 ./memory.test -test.run '^TestMemoryDrainIntegration\$$' -test.count=1 -test.v -test.timeout=30m"
	@"$(vm_script)" ssh "sudo env $(vm_memory_drain_env) LINUXIO_MEMORY_DRAIN_MIB=320 LINUXIO_MEMORY_DRAIN_READ_BPS=$(vm_memory_drain_rate) LINUXIO_MEMORY_DRAIN_MIN_SECONDS=90 ./memory.test -test.run '^TestMemoryDrainIntegration\$$' -test.count=1 -test.v -test.timeout=30m"

vm-test-filebrowser-root: ensure-go ## Run the root-only file browser ownership tests in the test VM
	@set -e
	@mkdir -p "$(cache_dir)/vm"
	@cd "$(backend_dir)" && $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" "$(GO_BIN)" test -c -o "$(cache_dir)/vm/filebrowser.test" ./bridge/handlers/filebrowser
	@cd "$(backend_dir)" && $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" "$(GO_BIN)" test -c -o "$(cache_dir)/vm/filebrowser-services.test" ./bridge/handlers/filebrowser/services
	@"$(vm_script)" ssh 'cat > filebrowser.test && chmod +x filebrowser.test' < "$(cache_dir)/vm/filebrowser.test"
	@"$(vm_script)" ssh 'cat > filebrowser-services.test && chmod +x filebrowser-services.test' < "$(cache_dir)/vm/filebrowser-services.test"
	@"$(vm_script)" ssh "sudo ./filebrowser.test -test.run $$(printf %q '$(FILEBROWSER_ROOT_RUN)') -test.count=1 -test.v"
	@"$(vm_script)" ssh "sudo ./filebrowser-services.test -test.run $$(printf %q '$(FILEBROWSER_ROOT_RUN)') -test.count=1 -test.v"

# The bridge-to-job-host relay tests (backend/bridge/cmd/jobs_relay_test.go).
# They serve a host in-process, so as root its owner and privileged rules are
# those of the root host; the relay dials it without starting a socket unit.
RELAY_ROOT_RUN := ^Test(Bridge(Forwards|Fails|Watch|Events|Reports)|PrivilegedBridgeUsesRootHost|Relay|UpdatePendingMessage)

vm-test-relay-root: ensure-go ## Run the bridge-to-job-host relay tests as root in the test VM
	@set -e
	@mkdir -p "$(cache_dir)/vm"
	@cd "$(backend_dir)" && $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" "$(GO_BIN)" test -c -o "$(cache_dir)/vm/bridge-cmd.test" ./bridge/cmd
	@"$(vm_script)" ssh 'cat > bridge-cmd.test && chmod +x bridge-cmd.test' < "$(cache_dir)/vm/bridge-cmd.test"
	@"$(vm_script)" ssh "sudo ./bridge-cmd.test -test.run $$(printf %q '$(RELAY_ROOT_RUN)') -test.count=1 -test.v"

test-vm-browser: ensure-node setup-frontend ## Run the Playwright specs in frontend/src/test/vm against the test VM
	@set -e
	@ip="$$("$(vm_script)" ip)"
	@cd "$(frontend_dir)" && \
	  LINUXIO_VM_URL="https://$$ip:8090" LINUXIO_VM_USER=linuxio-e2e LINUXIO_VM_PASSWORD="$$(cat "$(vm_state_dir)/password")" \
	  ./node_modules/.bin/playwright test --config config/playwright.vm.config.ts $(PLAYWRIGHT_ARGS)
