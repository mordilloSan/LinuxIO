# The disposable test VM (linuxio-e2e on qemu:///system) and the tests that run
# in it. packaging/vm/linuxio-vm.sh does the work; its state lives in
# ~/.cache/linuxio-vm, never in git. docs/development.md, "Test VM".

vm_script := $(repo_root)/packaging/vm/linuxio-vm.sh
# VM_RELEASE=noble selects the second VM (linuxio-e2e-noble, Ubuntu 24.04),
# kept for the root tests on an older kernel.
VM_RELEASE ?=
export LINUXIO_VM_RELEASE := $(VM_RELEASE)
# There can be several VMs of a release, numbered from 1, so the browser specs
# run on all of them at once. VM=2 names one: vm-create, vm-destroy, vm-ssh and
# the vm-test-* targets act on that one, the first without it. The others act
# on VMs 1 to VMS, or on the one VM names. test-vm sets VMS to as many as the
# host has free memory and CPUs for and creates the missing ones; for the
# rest it is every VM that exists, whatever memory is free, so a reset, an
# install and a run agree. Each is asked of the script once; when the script
# fails it has said why, and 1 lets the recipe fail on the same cause.
VM ?=
export LINUXIO_VM_INDEX := $(VM)
# A command run on several VMs keeps each VM's output in vm-N-COMMAND.log
# here, and the browser specs every line of theirs in test-vm.log.
export LINUXIO_VM_LOG_DIR = $(cache_dir)/test-logs
vm_fit = $(eval vm_fit := $(or $(shell "$(vm_script)" fit),1))$(vm_fit)
vm_count = $(eval vm_count := $(or $(shell "$(vm_script)" count),1))$(vm_count)
VMS ?= $(vm_count)
vm_all = "$(vm_script)" $(if $(VM),,each $(VMS))
vm_numbers = $(if $(VM),$(VM),$(shell seq $(VMS)))
# Every target that starts a VM shuts it down when it ends, passed or failed,
# so nothing keeps running unused; the next target starts it again. VM_KEEP=1
# leaves it up, to look at a failure while its processes still run.
VM_KEEP ?=
vm_stop := $(if $(VM_KEEP),true,"$(vm_script)" stop)
vm_stop_all = $(if $(VM_KEEP),true,$(vm_all) stop)
# One VM target at a time: a second one would revert, reboot or stop a VM
# under the first. It is the first line of a recipe, ahead of the trap that
# stops the VMs, so a refused target stops nothing.
vm_lock_file := $(HOME)/.cache/linuxio-vm/lock
vm_lock := mkdir -p "$(dir $(vm_lock_file))"; exec 9>"$(vm_lock_file)"; flock -n 9 || { echo "another VM target is running; wait for it to end" >&2; exit 1; }

##@ Test VM|yellow
vm-create: ## Create the test VM (VM=2 for one more) from the Ubuntu 26.04 cloud image and snapshot it as clean
	@$(vm_lock)
	@"$(vm_script)" create

vm-install: build-nocheck ## Build LinuxIO and install it in the test VMs
	@$(vm_lock)
	@trap '$(vm_stop_all)' EXIT
	@$(vm_all) install "$(bin_dir)"

# With VM_KEEP=1 it stops nothing, so it may look into a VM another target uses.
vm-ssh: ## Run CMD='...' in the test VM over SSH (no CMD: a shell)
	@$(if $(VM_KEEP),,$(vm_lock))
	@trap '$(vm_stop)' EXIT
	@"$(vm_script)" ssh $${CMD:+"$$CMD"}

vm-stop: ## Shut the test VMs down; their disks keep what is installed
	@$(vm_lock)
	@$(vm_all) stop

vm-reset: ## Revert the test VMs to their clean snapshot and stop them
	@$(vm_lock)
	@$(vm_all) reset

vm-update: ## Upgrade the packages in the test VMs' clean snapshot and take the snapshot again
	@$(vm_lock)
	@$(vm_all) update

vm-destroy: ## Delete the test VM (VM=2 for another) and its volumes
	@$(vm_lock)
	@"$(vm_script)" destroy

vm-test-btrfs-integration: ensure-go ## Run the root btrfs and partition kernel tests in the test VM
	@set -e
	@$(vm_lock)
	@trap '$(vm_stop)' EXIT
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
	@$(vm_lock)
	@trap '$(vm_stop)' EXIT
	@mkdir -p "$(cache_dir)/vm"
	@cd "$(backend_dir)" && $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" CGO_ENABLED=0 "$(GO_BIN)" test -c -o "$(cache_dir)/vm/memory.test" ./bridge/handlers/memory
	@"$(vm_script)" ssh '$(vm_memory_zram_setup)'
	@"$(vm_script)" ssh 'set -e; if ! grep -qs "^/swap.img[[:space:]]" /etc/fstab; then sudo fallocate -l 1G /swap.img && sudo chmod 600 /swap.img && sudo mkswap /swap.img >/dev/null && echo "/swap.img none swap sw 0 0" | sudo tee -a /etc/fstab >/dev/null && sudo systemctl daemon-reload && sudo systemctl start swap.img.swap; fi'
	@"$(vm_script)" ssh 'cat > memory.test && chmod +x memory.test' < "$(cache_dir)/vm/memory.test"
	@"$(vm_script)" ssh "echo \"VM kernel: \$$(uname -r)\" && sudo env LINUXIO_RUN_MEMORY_INTEGRATION=1 ./memory.test -test.run '^TestMemoryIntegration\$$' -test.count=1 -test.v -test.timeout=30m"
	@trap '"$(vm_script)" ssh "sudo systemctl stop dev-vdb.swap" || echo "/dev/vdb still swaps in the VM: run make vm-reset" >&2; $(vm_stop)' EXIT
	@"$(vm_script)" ssh '$(vm_memory_drain_setup)'
	@"$(vm_script)" ssh "sudo env $(vm_memory_drain_env) LINUXIO_MEMORY_DRAIN_MIB=2048 ./memory.test -test.run '^TestMemoryDrainIntegration\$$' -test.count=1 -test.v -test.timeout=30m"
	@"$(vm_script)" ssh "sudo env $(vm_memory_drain_env) LINUXIO_MEMORY_DRAIN_MIB=320 LINUXIO_MEMORY_DRAIN_READ_BPS=$(vm_memory_drain_rate) LINUXIO_MEMORY_DRAIN_MIN_SECONDS=90 ./memory.test -test.run '^TestMemoryDrainIntegration\$$' -test.count=1 -test.v -test.timeout=30m"

vm-test-filebrowser-root: ensure-go ## Run the root-only file browser ownership tests in the test VM
	@set -e
	@$(vm_lock)
	@trap '$(vm_stop)' EXIT
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
	@$(vm_lock)
	@trap '$(vm_stop)' EXIT
	@mkdir -p "$(cache_dir)/vm"
	@cd "$(backend_dir)" && $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" "$(GO_BIN)" test -c -o "$(cache_dir)/vm/bridge-cmd.test" ./bridge/cmd
	@"$(vm_script)" ssh 'cat > bridge-cmd.test && chmod +x bridge-cmd.test' < "$(cache_dir)/vm/bridge-cmd.test"
	@"$(vm_script)" ssh "sudo ./bridge-cmd.test -test.run $$(printf %q '$(RELAY_ROOT_RUN)') -test.count=1 -test.v"

# One Playwright worker for each VM: a worker takes the next spec file and
# keeps to its own VM, which the three lists name in the same order.
define vm_browser_specs
$(vm_all) start
urls="" passwords=""
for n in $(vm_numbers); do
  urls="$$urls https://$$(LINUXIO_VM_INDEX=$$n "$(vm_script)" ip):8090"
  passwords="$$passwords $$(LINUXIO_VM_INDEX=$$n "$(vm_script)" password)"
done
cd "$(frontend_dir)" && \
  LINUXIO_VM_NUMBERS="$(vm_numbers)" LINUXIO_VM_URLS="$$urls" LINUXIO_VM_PASSWORDS="$$passwords" LINUXIO_VM_USER=linuxio-e2e \
  ./node_modules/.bin/playwright test --config config/playwright.vm.config.ts $(PLAYWRIGHT_ARGS)
endef

test-vm-browser: ensure-node setup-frontend ## Run the Playwright specs in frontend/src/test/vm against the test VMs as they are
	@set -e
	@$(vm_lock)
	@trap '$(vm_stop_all)' EXIT
	@$(vm_browser_specs)

# The whole run in one target: as many VMs as the host has room for (VMS=2
# for fewer), each created when it is missing, reverted to clean and given
# this build, then the specs.
test-vm: VMS = $(vm_fit)
test-vm: build-nocheck ensure-node setup-frontend ## Create the test VMs the host has room for, revert them, install this build and run the browser specs
	@set -e
	@$(vm_lock)
	@trap '$(vm_stop_all)' EXIT
	@echo "linuxio-vm: $(if $(VM),VM $(VM),$(VMS) VM(s)) for this run; $$("$(vm_script)" room)"
	@$(vm_all) prepare "$(bin_dir)"
	@$(vm_browser_specs)
