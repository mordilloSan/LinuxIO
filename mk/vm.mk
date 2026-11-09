# The disposable test VM (linuxio-e2e on qemu:///system) and the tests that run
# in it. packaging/vm/linuxio-vm.sh does the work; its state lives in
# ~/.cache/linuxio-vm, never in git. docs/development.md, "Test VM".

vm_script := $(repo_root)/packaging/vm/linuxio-vm.sh
vm_state_dir := $(HOME)/.cache/linuxio-vm

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
	@cd "$(backend_dir)" && $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" "$(GO_BIN)" test -c -o "$(cache_dir)/vm/btrfs.test" ./common/btrfs
	@"$(vm_script)" ssh 'cat > btrfs.test && chmod +x btrfs.test' < "$(cache_dir)/vm/btrfs.test"
	@"$(vm_script)" ssh "echo \"VM kernel: \$$(uname -r)\" && sudo env LINUXIO_RUN_BTRFS_INTEGRATION=1 ./btrfs.test -test.run $$(printf %q '$(BTRFS_INTEGRATION_RUN)') -test.count=1 -test.v -test.timeout=20m"

vm-test-filebrowser-root: ensure-go ## Run the root-only file browser ownership tests in the test VM
	@set -e
	@mkdir -p "$(cache_dir)/vm"
	@cd "$(backend_dir)" && $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" "$(GO_BIN)" test -c -o "$(cache_dir)/vm/filebrowser.test" ./bridge/handlers/filebrowser
	@cd "$(backend_dir)" && $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" "$(GO_BIN)" test -c -o "$(cache_dir)/vm/filebrowser-services.test" ./bridge/handlers/filebrowser/services
	@"$(vm_script)" ssh 'cat > filebrowser.test && chmod +x filebrowser.test' < "$(cache_dir)/vm/filebrowser.test"
	@"$(vm_script)" ssh 'cat > filebrowser-services.test && chmod +x filebrowser-services.test' < "$(cache_dir)/vm/filebrowser-services.test"
	@"$(vm_script)" ssh "sudo ./filebrowser.test -test.run $$(printf %q '$(FILEBROWSER_ROOT_RUN)') -test.count=1 -test.v"
	@"$(vm_script)" ssh "sudo ./filebrowser-services.test -test.run $$(printf %q '$(FILEBROWSER_ROOT_RUN)') -test.count=1 -test.v"

test-vm-browser: ensure-node setup ## Run the Playwright specs in frontend/src/test/vm against the test VM
	@set -e
	@ip="$$("$(vm_script)" ip)"
	@cd "$(frontend_dir)" && \
	  LINUXIO_VM_URL="https://$$ip:8090" LINUXIO_VM_USER=linuxio-e2e LINUXIO_VM_PASSWORD="$$(cat "$(vm_state_dir)/password")" \
	  ./node_modules/.bin/playwright test --config config/playwright.vm.config.ts $(PLAYWRIGHT_ARGS)
