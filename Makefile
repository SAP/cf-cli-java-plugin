JAVA_PLUGIN_INSTALLED = $(cf plugins | grep -q)
JSTALL_JAR = dist/jstall-minimal.jar
# Set JSTALL_DEV=1 to pull the latest GitHub Actions build instead of the latest release
JSTALL_DEV ?=
# Set JSTALL_LOCAL=/path/to/jstall.jar to use a locally built JAR instead of downloading
JSTALL_LOCAL ?=

all: install

build: compile

$(JSTALL_JAR):
	mkdir -p dist
ifdef JSTALL_LOCAL
	cp $(JSTALL_LOCAL) $@
else ifdef JSTALL_DEV
ifeq ($(JSTALL_DEV),1)
	gh run download -R parttimenerd/jstall -n jstall-minimal-jar --dir dist
else
	gh run download $(JSTALL_DEV) -R parttimenerd/jstall -n jstall-minimal-jar --dir dist
endif
else
	curl -sL -o $@ https://github.com/parttimenerd/jstall/releases/latest/download/jstall-minimal.jar
endif

download-jstall: $(JSTALL_JAR)

update-jstall:
	rm -f $(JSTALL_JAR)
	$(MAKE) download-jstall

.PHONY: build compile compile-all update-jstall download-jstall download-hprof-redact update-hprof-redact install remove clean vclean

# When JSTALL_LOCAL or JSTALL_DEV is set, always re-copy/re-download (skip file existence check)
ifdef JSTALL_LOCAL
JSTALL_DEP = update-jstall
else ifdef JSTALL_DEV
JSTALL_DEP = update-jstall
else
JSTALL_DEP = $(JSTALL_JAR)
endif

# ── hprof-redact embedded binaries ───────────────────────────────────────────
# Downloaded at compile time from hprof-analyzer GitHub releases.
# Uses musl-static Linux builds so the binary runs in CF containers without
# glibc version constraints.
HPROF_REDACT_BASE = https://github.com/parttimenerd/hprof-analyzer/releases/download/nightly

dist/hprof-redact-linux-amd64:
	mkdir -p dist
	curl -sL $(HPROF_REDACT_BASE)/hprof-analyzer-x86_64-unknown-linux-musl.tar.gz \
	  | tar -xz --strip-components=1 -C dist hprof-analyzer-x86_64-unknown-linux-musl/hprof-redact
	mv dist/hprof-redact $@

dist/hprof-redact-linux-arm64:
	mkdir -p dist
	curl -sL $(HPROF_REDACT_BASE)/hprof-analyzer-aarch64-unknown-linux-musl.tar.gz \
	  | tar -xz --strip-components=1 -C dist hprof-analyzer-aarch64-unknown-linux-musl/hprof-redact
	mv dist/hprof-redact $@

dist/hprof-redact-darwin-arm64:
	mkdir -p dist
	curl -sL $(HPROF_REDACT_BASE)/hprof-analyzer-aarch64-apple-darwin.tar.gz \
	  | tar -xz --strip-components=1 -C dist hprof-analyzer-aarch64-apple-darwin/hprof-redact
	mv dist/hprof-redact $@

dist/hprof-redact-windows-amd64.exe:
	mkdir -p dist
	$(eval WINTMP := $(shell mktemp -d))
	curl -sL $(HPROF_REDACT_BASE)/hprof-analyzer-x86_64-pc-windows-msvc.zip -o $(WINTMP)/win.zip
	cd $(WINTMP) && unzip -o win.zip hprof-analyzer-x86_64-pc-windows-msvc/hprof-redact.exe
	cp $(WINTMP)/hprof-analyzer-x86_64-pc-windows-msvc/hprof-redact.exe $@
	rm -rf $(WINTMP)

dist/hprof-redact-windows-arm64.exe:
	mkdir -p dist
	$(eval WINTMP := $(shell mktemp -d))
	curl -sL -f $(HPROF_REDACT_BASE)/hprof-analyzer-aarch64-pc-windows-msvc.zip -o $(WINTMP)/win.zip \
	  && cd $(WINTMP) && unzip -o win.zip hprof-analyzer-aarch64-pc-windows-msvc/hprof-redact.exe \
	  && cp $(WINTMP)/hprof-analyzer-aarch64-pc-windows-msvc/hprof-redact.exe $@ \
	  || touch $@
	rm -rf $(WINTMP)

HPROF_REDACT_BINS = \
	dist/hprof-redact-linux-amd64 \
	dist/hprof-redact-linux-arm64 \
	dist/hprof-redact-darwin-arm64 \
	dist/hprof-redact-windows-amd64.exe \
	dist/hprof-redact-windows-arm64.exe

download-hprof-redact: $(HPROF_REDACT_BINS)

update-hprof-redact:
	rm -f $(HPROF_REDACT_BINS)
	$(MAKE) download-hprof-redact

compile: $(JSTALL_DEP) $(HPROF_REDACT_BINS)
	go build -o build/cf-cli-java-plugin .

compile-all: $(JSTALL_DEP) $(HPROF_REDACT_BINS)
	GOOS=linux GOARCH=amd64 go build -o build/cf-cli-java-plugin-linux64 .
	GOOS=linux GOARCH=arm64 go build -o build/cf-cli-java-plugin-linux-arm64 .
	GOOS=darwin GOARCH=arm64 go build -o build/cf-cli-java-plugin-osx-arm64 .
	GOOS=windows GOARCH=amd64 go build -o build/cf-cli-java-plugin-win64.exe .
	GOOS=windows GOARCH=arm64 go build -o build/cf-cli-java-plugin-win-arm64.exe .

clean:
	rm -r build

install: compile remove
	yes | cf install-plugin build/cf-cli-java-plugin

remove: $(objects)
ifeq ($(JAVA_PLUGIN_INSTALLED),)
	cf uninstall-plugin java || true
endif

vclean: remove clean