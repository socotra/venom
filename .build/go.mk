

GO_BUILD 			= CGO_ENABLED=0 go build -installsuffix cgo
GO_LIST 			= env GO111MODULE=on go list
TEST_C_CMD 			= go test -c
TEST_RUN_ARGS 		= -test.v -test.timeout 600s -test.coverprofile=profile.coverprofile
CURRENT_PACKAGE 	= $(shell $(GO_LIST))
TARGET_DIST 		:= ./dist
TARGET_RESULTS 		:= ./results
GOPATH := $(shell go env GOPATH)

PKGS_COMMA_SEP = go list -f '{{ join .Deps "\n" }}{{"\n"}}{{.ImportPath}}' . | grep github.com/ovh/venom | grep -v vendor | tr '\n' ',' | sed 's/,$$//'

##### =====> Clean <===== #####

mk_go_clean: # clean target directory
	@rm -rf $(TARGET_DIST)
	@rm -rf $(TARGET_RESULTS)
	@for testfile in `find ./ -name "bin.test"`; do \
		rm $$testfile; \
	done;
	@for TST in `find ./ -name "tests.log"`; do \
		rm $$TST; \
	done;
	@for profile in `find ./ -name "*.coverprofile"`; do \
		rm $$profile; \
	done;

##### =====> Compile <===== #####

IS_TEST                    = $(filter test,$(MAKECMDGOALS))
TARGET_OS                  = $(filter-out $(TARGET_OS_EXCLUDED), $(if ${ENABLE_CROSS_COMPILATION},$(if ${OS},${OS}, $(if $(IS_TEST), $(shell go env GOOS), windows darwin linux openbsd freebsd)),$(shell go env GOOS)))
TARGET_ARCH                = $(if ${ARCH},${ARCH}, $(if $(IS_TEST), $(shell go env GOARCH),amd64 arm 386 arm64 ppc64le))
BINARIES                   = $(addprefix $(TARGET_DIST)/, $(addsuffix .$(OS)-$(ARCH)$(if $(IS_WINDOWS),.exe), $(notdir $(TARGET_NAME))))
OSARCHVALID                := $(shell go tool dist list | grep -v '^darwin/386'|grep -v '^windows/386'|grep -v '^windows/arm'|grep -v '^openbsd/arm*'|grep -v '^openbsd/386'|grep -v '^freebsd/arm*'|grep -v '^freebsd/386')
IS_OS_ARCH_VALID           = $(filter $(OS)/$(ARCH),$(OSARCHVALID))
CROSS_COMPILED_BINARIES    = $(foreach OS, $(TARGET_OS), $(foreach ARCH, $(TARGET_ARCH), $(if $(IS_OS_ARCH_VALID), $(BINARIES))))
GOFILES                    := $(call get_recursive_files, '.')

mk_go_build:
	$(info *** mk_go_build)

mk_go_build_plugin:
	@mkdir -p dist/lib && \
	go build -buildmode=plugin -o dist/lib/$(TARGET_NAME).so

mk_go_build_clean:
	@rm -rf dist

$(CROSS_COMPILED_BINARIES): $(GOFILES) $(TARGET_DIST)
	$(info *** compiling $@)
	@os=$(call get_os_from_binary_file,$@); \
	filename=$@ ; \
	if test "$${os}" = "windows"; then filename=$@.exe; fi; \
	GOOS=$${os} \
	GOARCH=$(call get_arch_from_binary_file,$@) \
	$(GO_BUILD) $(BUILD_MODE) $(LDFLAGS) -o $${filename};

##### =====> Compile Tests <===== #####

PKGS     = $(or $(PKG),$(shell $(GO_LIST) ./...))
TESTPKGS = $(shell $(GO_LIST) -f \
			'{{ if or .TestGoFiles .XTestGoFiles }}{{ .ImportPath }}{{ end }}' \
			$(PKGS))

TESTPKGS_C_FILE = $(addsuffix /bin.test, $(subst $(CURRENT_PACKAGE),.,$(PKG)))
TESTPKGS_C = $(foreach PKG, $(TESTPKGS), $(TESTPKGS_C_FILE))

$(TESTPKGS_C): #main_test.go
	$(info *** compiling test $@)
	@cd $(dir $@); \
	TEMP=`$(PKGS_COMMA_SEP)`; \
	$(TEST_C_CMD) -coverpkg $$TEMP -o bin.test .;

##### =====> Running Tests <===== #####

TESTPKGS_RESULTS_LOG_FILE = $(addsuffix /tests.log, $(subst $(CURRENT_PACKAGE),.,$(PKG)))
TESTPKGS_RESULTS = $(foreach PKG, $(TESTPKGS), $(TESTPKGS_RESULTS_LOG_FILE))

$(HOME)/.richstyle.yml:
	echo "leaveTestPrefix: true" > $(HOME)/.richstyle.yml

GO_RICHGO := $(GOPATH)/bin/richgo
$(GO_RICHGO): $(HOME)/.richstyle.yml
	go install github.com/kyoh86/richgo@latest

EXIT_TESTS := 0
$(TESTPKGS_RESULTS): $(GOFILES) $(TESTPKGS_C) $(GO_RICHGO)
	$(info *** executing tests in $(dir $@))
	@-cd $(dir $@) && ./bin.test $(TEST_RUN_ARGS) | tee tests.log | richgo testfilter ;

GO_COV_MERGE := $(GOPATH)/bin/gocovmerge
$(GO_COV_MERGE):
	go install github.com/wadey/gocovmerge@latest

GO_GOJUNIT := $(GOPATH)/bin/go-junit-report
$(GO_GOJUNIT):
	go install github.com/jstemmer/go-junit-report@latest

GO_COBERTURA := $(GOPATH)/bin/gocover-cobertura
$(GO_COBERTURA):
	go install github.com/t-yuki/gocover-cobertura@latest

GO_XUTOOLS := $(GOPATH)/bin/xutools
$(GO_XUTOOLS):
	go install github.com/richardlt/xutools@latest

GO_GOIMPORTS = ${GOPATH}/bin/goimports
$(GO_GOIMPORTS):
	go install golang.org/x/tools/cmd/goimports@latest

mk_go_test: $(GO_COV_MERGE) $(GO_COBERTURA) $(GOFILES) $(TARGET_RESULTS) $(TESTPKGS_RESULTS)# Run tests
	@echo "Generating unit tests coverage..."
	@$(GO_COV_MERGE) `find ./ -name "*.coverprofile"` > $(TARGET_RESULTS)/cover.out
	@$(GO_COBERTURA) < $(TARGET_RESULTS)/cover.out > $(TARGET_RESULTS)/coverage.xml
	@go tool cover -html=$(TARGET_RESULTS)/cover.out -o=$(TARGET_RESULTS)/cover.html
	@NB=$$(grep "^FAIL" `find . -type f -name "tests.log"`|grep -v ':0'|wc -l); echo "tests failed $$NB" && exit $$NB

mk_go_test-xunit: $(GO_GOJUNIT) $(GO_XUTOOLS) $(TARGET_RESULTS) # Generate test with xunit report
	@echo "Generating xUnit Report..."
	@for TST in `find . -name "tests.log"`; do \
		if [ -s $$TST ]; then \
			FAILED=`grep -E '(FAIL)+\s([a-z\.\/]*)\s\[build failed\]' $$TST | wc -l`; \
			if [ $$FAILED -gt 0 ]; then \
				echo "Build Failed \t\t\t($$TST)"; \
				echo "Build Failed \t\t\t($$TST)" >>  $(TARGET_RESULTS)/fail; \
			else \
				NO_TESTS=`grep -E '\?+\s+([a-z\.\/]*)\s\[no test files\]' $$TST | wc -l`; \
				if [ $$NO_TESTS -gt 0 ]; then \
					echo "No tests found \t\t\t($$TST)"; \
				else \
					PACKAGE=venom_`echo $$TST | sed 's|./||' | sed 's|/|_|g' | sed 's|_tests.log||'`; \
					TESTS_LOG_OUT=$(TARGET_RESULTS)/$$PACKAGE.log; \
					cp $$TST $$TESTS_LOG_OUT; \
					if [ ! -z "${CDS_VERSION}" ]; then \
						echo "Sending $$TESTS_LOG_OUT to CDS"; \
						worker upload --tag `echo $$TST | sed 's|./||' | sed 's|./||' | sed 's|/|_|g') | sed 's|_tests.log||'` $(abspath $$TESTS_LOG_OUT); \
					fi; \
					echo "Generating xUnit report \t$$TST.tests-results.xml"; \
					cat $$TST | $(GO_GOJUNIT) > $$TST.tests-results.xml; \
				fi; \
			fi; \
		else \
			echo "Ignoring empty file \t\t$$TST"; \
		fi; \
	done; \
	for XML in `find . -name "tests.log.tests-results.xml"`; do \
		if [ "$$XML" =  "./tests.log.tests-results.xml" ]; then \
			PWD=`pwd`; \
			mv $$XML $(TARGET_RESULTS)/`basename $(PWD)`.tests-results.xml; \
		else \
			mv $$XML $(TARGET_RESULTS)/`echo $$XML | sed 's|./||' | sed 's|/|_|g' | sed 's|_tests.log||'`; \
		fi; \
	done; \
	xutools pretty --show-failures $(TARGET_RESULTS)/*.xml > $(TARGET_RESULTS)/report; \
	xutools sort-duration $(TARGET_RESULTS)/*.xml > $(TARGET_RESULTS)/duration; \
	if [ -e $(TARGET_RESULTS)/report ]; then \
		echo "Report:"; \
		cat $(TARGET_RESULTS)/report; \
	fi; \
	if [ -e $(TARGET_RESULTS)/duration ]; then \
		echo "Max duration:"; \
		cat $(TARGET_RESULTS)/duration; \
	fi; \
	if [ -e $(TARGET_RESULTS)/fail ]; then \
		echo "#########################"; \
		echo "ERROR: Test compilation failure"; \
		cat $(TARGET_RESULTS)/fail; \
		exit 1; \
	fi;

##### =====> lint <===== #####

TMP_DIR = /tmp/ovh/venom

OSNAME=$(shell go env GOOS)
LINT_ARCH = $(shell go env GOARCH)
CUR_PATH = $(notdir $(shell pwd))

LINT_DIR = $(TMP_DIR)/$(CUR_PATH)/golangci-lint
LINT_BIN = $(LINT_DIR)/golangci-lint

LINT_VERSION=1.31.0
LINT_CMD = $(LINT_BIN) run --allow-parallel-runners -c .golangci.yml --build-tags $(OSNAME)
LINT_ARCHIVE = golangci-lint-$(LINT_VERSION)-$(OSNAME)-$(LINT_ARCH).tar.gz
LINT_ARCHIVE_DEST = $(TMP_DIR)/$(LINT_ARCHIVE)

# Run this on localc machine.
# It downloads a version of golangci-lint and execute it locally.
# duration first time ~6s
# duration second time ~2s
.PHONY: lint
lint: $(LINT_BIN)
	$(LINT_DIR)/$(LINT_CMD)

# install a local golangci-lint if not found.
$(LINT_BIN):
	curl -L --create-dirs \
		--retry 6 \
		--retry-delay 9 \
		--retry-max-time 60 \
		https://github.com/golangci/golangci-lint/releases/download/v$(LINT_VERSION)/$(LINT_ARCHIVE) \
		--output $(LINT_ARCHIVE_DEST)
	mkdir -p $(LINT_DIR)/
	tar -xf $(LINT_ARCHIVE_DEST) --strip-components=1 -C $(LINT_DIR)/
	chmod +x $(LINT_BIN)
	rm -f $(LINT_ARCHIVE_DEST)

mk_go_lint: $(GOLANG_CI_LINT) # run golangci lint
	$(info *** running lint)
	$(LINT_CMD)

.PHONY: gofmt
gofmt:
	gofmt -e -l -s -w .

.PHONY: goimports
goimports:
	$(GO_GOIMPORTS) -e -l -w -local github.com/ovh/venom .

.PHONY: fmt
fmt: gofmt goimports

##### =====> Internals <===== #####

$(TARGET_RESULTS):
	$(info create $(TARGET_RESULTS) directory)
	@mkdir -p $(TARGET_RESULTS)

$(TARGET_DIST):
	$(info create $(TARGET_DIST) directory)
	@mkdir -p $(TARGET_DIST)

define get_os_from_binary_file
$(strip $(shell echo $(1) | cut -d '.' -f 2 | cut -d'-' -f 1))
endef

define get_arch_from_binary_file
$(strip $(patsubst %.exe, %,$(shell echo $(1) | cut -d'-' -f 2)))
endef

define get_recursive_files
$(subst ./,,$(shell find $(1) -type f -name "*.go" -print))
endef
