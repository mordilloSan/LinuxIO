# The disposable test VM (linuxio-e2e on qemu:///system) and the tests that run
# in it. packaging/vm/linuxio-vm.sh does the work; its state lives in
# ~/.cache/linuxio-vm, never in git. docs/development.md, "Test VM".

vm_script := $(repo_root)/packaging/vm/linuxio-vm.sh
# DISTRO=fedora selects the distribution, a profile in the script: Fedora's
# VMs are linuxio-e2e-fedora and ~/.cache/linuxio-vm-fedora, beside Ubuntu's.
# Without it, Ubuntu and the names it always had.
DISTRO ?=
export LINUXIO_VM_DISTRO := $(DISTRO)
# VM_RELEASE=noble selects the second Ubuntu VM (linuxio-e2e-noble, Ubuntu
# 24.04), kept for the root tests on an older kernel.
VM_RELEASE ?=
export LINUXIO_VM_RELEASE := $(VM_RELEASE)
# There can be several VMs of a release, numbered from 1, so the browser specs
# run on all of them at once. VM=2 names one: vm-create, vm-destroy, vm-ssh and
# the vm-test-* targets act on that one, the first without it. The others act
# on VMs 1 to VMS, or on the one VM names. test-vm-browser and test-vm-backend
# set VMS to as many as the host has free memory and CPUs for (test-vm-backend
# to no more than it has suites) and create the missing ones; for the rest it
# is every VM that exists, whatever memory is free, so a reset, an install and
# a run agree. Each is asked of the script once; when the script
# fails it has said why, and 1 lets the recipe fail on the same cause.
VM ?=
export LINUXIO_VM_INDEX := $(VM)
# A command run on several VMs keeps each VM's output in vm-N-COMMAND.log
# here, and the browser specs every line of theirs in test-vm-browser.log
# (test-vm-browser-release-update.log for the release-update project).
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
vm-create: ## Create the test VM (VM=2 for one more) from the Ubuntu 26.04 cloud image (DISTRO=fedora: Fedora 45) and snapshot it as clean
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

# The root test suites. Make builds their test binaries into vm_test_dir and
# the script's test command copies them into the VM and runs them as root,
# with the test name patterns the host targets use (quality.mk). The binaries
# are static (CGO_ENABLED=0), so they also run on the older glibc of
# VM_RELEASE=noble. A suite is a suite_NAME function in the script and a
# vm_build_NAME here (indexer installs the LinuxIO build instead), listed in
# vm_suites longest first. test-vm-backend writes SUITES into a queue the VMs
# share, each taking the next suite when it is free, so the long ones start
# first and the short ones fill in around them.
vm_test_dir := $(cache_dir)/vm
vm_suites := memory btrfs indexer docker relay updater userconfig filebrowser network
SUITES ?= $(vm_suites)
# The bridge-to-job-host relay tests (backend/bridge/cmd/jobs_relay_test.go).
RELAY_ROOT_RUN := ^Test(Bridge(Forwards|Fails|Watch|Events|Reports)|PrivilegedBridgeUsesRootHost|Relay|UpdatePendingMessage)
# The memory suite's zram tool, the profile's when empty: zram-tools on Ubuntu,
# zram-generator on Fedora. ZRAM_PROVIDER=zram-generator on Ubuntu runs it with
# systemd's zram-generator, the Fedora and RHEL path, on a VM without
# zram-tools (run make vm-reset first). docs/memory.md, "Tests".
ZRAM_PROVIDER ?=
export LINUXIO_VM_BTRFS_RUN := $(BTRFS_INTEGRATION_RUN)
export LINUXIO_VM_FILEBROWSER_RUN := $(FILEBROWSER_ROOT_RUN)
export LINUXIO_VM_RELAY_RUN := $(RELAY_ROOT_RUN)
export LINUXIO_VM_UPDATER_RUN := $(UPDATER_RUN)
export LINUXIO_VM_DOCKER_RUN := $(DOCKER_UPDATE_RUN)
export LINUXIO_VM_ZRAM_PROVIDER := $(ZRAM_PROVIDER)
export LINUXIO_VM_BUILD_DIR := $(bin_dir)
# $(call vm_test_build,NAME,PACKAGE) builds PACKAGE's tests as NAME.test.
# Each build is a command of its own, ended by ;, so set -e stops at a failed
# one: it would let a failure early in an && list pass.
vm_test_build = (cd "$(backend_dir)" && $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" CGO_ENABLED=0 "$(GO_BIN)" test -c -o "$(vm_test_dir)/$(1).test" $(2));
vm_build_btrfs = $(call vm_test_build,btrfs,./common/btrfs) $(call vm_test_build,storage,./bridge/handlers/storage)
vm_build_memory = $(call vm_test_build,memory,./bridge/handlers/memory)
vm_build_filebrowser = $(call vm_test_build,filebrowser,./bridge/handlers/filebrowser) $(call vm_test_build,filebrowser-services,./bridge/handlers/filebrowser/services)
vm_build_relay = $(call vm_test_build,bridge-cmd,./bridge/cmd)
vm_build_updater = $(call vm_test_build,appupdate,./bridge/handlers/appupdate)
vm_build_docker = $(call vm_test_build,dockerupdate,./common/dockerupdate)
vm_build_network = $(call vm_test_build,hostbridge,./bridge/handlers/network/internal/hostbridge)
vm_build_userconfig = $(call vm_test_build,userconfig,./bridge/internal/userconfig)
# A wrong SUITES fails as make reads this file, before the build the indexer
# suite brings, and only for test-vm-backend.
ifneq ($(filter test-vm-backend%,$(MAKECMDGOALS)),)
ifeq ($(strip $(SUITES)),)
$(error SUITES is empty (the suites: $(vm_suites)))
endif
ifneq ($(filter-out $(vm_suites),$(SUITES)),)
$(error unknown SUITES: $(filter-out $(vm_suites),$(SUITES)) (the suites: $(vm_suites)))
endif
endif
# A wrong DISTRO, VM_RELEASE or ZRAM_PROVIDER fails the same way, before the
# builds of a VM target; the script checks them again.
ifneq ($(filter vm-% test-vm-%,$(MAKECMDGOALS)),)
ifneq ($(filter-out ubuntu fedora,$(DISTRO))$(word 2,$(DISTRO)),)
$(error unknown DISTRO '$(DISTRO)' (ubuntu, the default, or fedora))
endif
ifneq ($(filter-out resolute noble,$(VM_RELEASE))$(word 2,$(VM_RELEASE)),)
$(error unknown VM_RELEASE '$(VM_RELEASE)' (resolute, the default, or noble))
endif
ifneq ($(filter-out zram-tools zram-generator,$(ZRAM_PROVIDER))$(word 2,$(ZRAM_PROVIDER)),)
$(error unknown ZRAM_PROVIDER '$(ZRAM_PROVIDER)' (zram-tools or zram-generator))
endif
ifeq ($(strip $(DISTRO)),fedora)
ifneq ($(strip $(VM_RELEASE)),)
$(error VM_RELEASE is for Ubuntu; DISTRO=fedora has one release)
endif
ifeq ($(strip $(ZRAM_PROVIDER)),zram-tools)
$(error Fedora has no zram-tools: use ZRAM_PROVIDER=zram-generator, the default there)
endif
endif
endif

# $(call vm_suite,SUITE) is the recipe of one vm-test-* target.
define vm_suite
set -e
$(vm_lock)
trap '$(vm_stop)' EXIT
mkdir -p "$(vm_test_dir)"
$(vm_build_$(1))
"$(vm_script)" test "$(vm_test_dir)" $(1)
endef

vm-test-btrfs-integration: ensure-go ## Run the root btrfs and partition kernel tests in the test VM
	@$(call vm_suite,btrfs)

vm-test-memory-integration: ensure-go ## Run the root swap mode and drain tests in the test VM
	@$(call vm_suite,memory)

vm-test-filebrowser-root: ensure-go ## Run the root-only file browser ownership tests in the test VM
	@$(call vm_suite,filebrowser)

vm-test-relay-root: ensure-go ## Run the bridge-to-job-host relay tests as root in the test VM
	@$(call vm_suite,relay)

##@ Tests|green
# As many VMs as the host has room for, one for each suite at most, since a
# VM without a suite would only be reverted; VMS=1 runs them one after another
# on the first. Each is created when it is missing and reverted to clean, so a
# run starts from clean whatever ran before it, and the indexer suite finds no
# LinuxIO to refuse. The suites bring their own test binaries, so no build is
# installed. SUITES='docker updater' runs only those.
test-vm-backend: VMS = $(shell n=$(vm_fit) s=$(words $(SUITES)); echo $$((n < s ? n : s)))
test-vm-backend: ensure-go $(if $(filter indexer,$(SUITES)),build-nocheck) ## Create the test VMs the host has room for (one per suite at most), revert them and run the root suites (SUITES=...) on them, each VM taking the next when free
	@set -e
	@$(vm_lock)
	@trap '$(vm_stop_all)' EXIT
	@echo "linuxio-vm: $(if $(VM),VM $(VM),$(VMS) VM(s)) for this run; $$("$(vm_script)" room)"
	@mkdir -p "$(vm_test_dir)"
	@$(foreach suite,$(SUITES),$(vm_build_$(suite)))
	@printf '%s\n' $(SUITES) >"$(vm_test_dir)/queue"
	@$(vm_all) prepare
	@$(vm_all) test "$(vm_test_dir)"

# The update build the update specs install (jobs-phase5a.spec.ts,
# jobs-update.spec.ts): this tree with jobs.Protocol one higher, so a job host
# of the installed build is an older one to its bridge. A Go overlay of
# record.go raises it without touching the tree; the build is a copy of the
# plain one with its Go binaries built again. docs/jobs.md, Testing.
vm_update_dir := $(cache_dir)/vm-update-build
vm_update_build := $(vm_update_dir)/build
vm_record_go := $(backend_dir)/common/jobs/record.go

##@ Test VM|yellow
vm-update-build: build-nocheck ## Build this tree with jobs.Protocol one higher into .cache/vm-update-build/build, the build the update specs install
	@set -e
	@mkdir -p "$(vm_update_dir)"
	@perl -pe 's/^(\tProtocol\s+=\s+)(\d+)$$/$$1 . ($$2 + 1)/e' "$(vm_record_go)" >"$(vm_update_dir)/record.go"
	@if cmp -s "$(vm_record_go)" "$(vm_update_dir)/record.go"; then echo "vm-update-build: no jobs.Protocol line to raise in $(vm_record_go)" >&2; exit 1; fi
	@printf '{"Replace":{"%s":"%s"}}\n' "$(vm_record_go)" "$(vm_update_dir)/record.go" >"$(vm_update_dir)/overlay.json"
	@rm -rf "$(vm_update_build)"
	@cp -a "$(bin_dir)" "$(vm_update_build)"
	@$(MAKE) --no-print-directory build-go-binaries BIN_DIR="$(vm_update_build)" SKIP_ENSURE_GO=1 \
	  GO_BUILD_EXTRA_ENV="GOFLAGS='-buildvcs=false -overlay=$(vm_update_dir)/overlay.json'"

# $(call vm_browser_specs,NUMBERS,ARGS[,ENV]) runs Playwright with ARGS on the
# VMs NUMBERS, one worker for each: a worker takes the next spec file and keeps
# to its own VM, which the three lists name in the same order. ENV is more
# variables for it. The VMs are running: test-vm-browser's install boots them,
# the rerun starts them, and the ip command starts one that is stopped.
define vm_browser_specs
urls="" passwords=""
for n in $(1); do
  urls="$$urls https://$$(LINUXIO_VM_INDEX=$$n "$(vm_script)" ip):8090"
  passwords="$$passwords $$(LINUXIO_VM_INDEX=$$n "$(vm_script)" password)"
done
(cd "$(frontend_dir)" && \
  LINUXIO_VM_NUMBERS="$(1)" LINUXIO_VM_URLS="$$urls" LINUXIO_VM_PASSWORDS="$$passwords" LINUXIO_VM_USER=linuxio-e2e $(3) \
  ./node_modules/.bin/playwright test --config config/playwright.vm.config.ts $(2))
endef
# The specs project unless PLAYWRIGHT_ARGS names a project: release-update
# (jobs-update.spec.ts) needs a VM reverted to clean (playwright.vm.config.ts).
vm_specs_args = $(if $(findstring --project,$(PLAYWRIGHT_ARGS)),,--project=specs) $(PLAYWRIGHT_ARGS)

test-vm-browser-rerun: ensure-node setup-frontend ## Rerun the browser specs against the test VMs as they are, without reverting or installing (test-vm-browser does both)
	@set -e
	@$(vm_lock)
	@trap '$(vm_stop_all)' EXIT
	@$(vm_all) start
	@$(call vm_browser_specs,$(vm_numbers),$(vm_specs_args))

# The release-update project after the others, alone on the first VM of the
# run reverted to clean, with a log of its own.
define vm_release_update
LINUXIO_VM_INDEX=$(firstword $(vm_numbers)) "$(vm_script)" reset
$(call vm_browser_specs,$(firstword $(vm_numbers)),--project=release-update,LINUXIO_VM_LOG=test-vm-browser-release-update.log)
endef

##@ Tests|green
# The whole run in one target: as many VMs as the host has room for (VMS=2
# for fewer), each created when it is missing, reverted to clean and given
# this build, then the specs, then the release-update spec. A run narrowed
# with PLAYWRIGHT_ARGS runs those specs only.
test-vm-browser: VMS = $(vm_fit)
test-vm-browser: export LINUXIO_VM_UPDATE_BUILD = $(vm_update_build)
test-vm-browser: build-nocheck vm-update-build ensure-node setup-frontend ## Create the test VMs the host has room for, revert them, install this build and run the browser specs, then the release-update spec on a VM reverted again
	@set -e
	@$(vm_lock)
	@trap '$(vm_stop_all)' EXIT
	@echo "linuxio-vm: $(if $(VM),VM $(VM),$(VMS) VM(s)) for this run; $$("$(vm_script)" room)"
	@$(vm_all) prepare "$(bin_dir)"
	@$(call vm_browser_specs,$(vm_numbers),$(vm_specs_args))
	@$(if $(PLAYWRIGHT_ARGS),,$(vm_release_update))
