# The three frontends over one core: the CLI, the TUI, and the Firefox host.
CMDS := ./cmd/wd ./cmd/wdt ./cmd/wd-host

.PHONY: all build extension install-firefox uninstall-firefox install-core test vet fmt check run clean

all: build

build:
	go build $(CMDS)

extension:
	mkdir -p build
	rm -f build/workdash-firefox.xpi
	cd browser/firefox && zip -qr ../../build/workdash-firefox.xpi .

# install-firefox installs the daemon-free host. There is no socket and no systemd
# unit: Firefox spawns wd-host per message and it rebuilds the snapshot the way wd
# does.
install-firefox: extension
	bash scripts/install-firefox-host.sh

uninstall-firefox:
	bash scripts/uninstall-firefox.sh

# install-core installs both new frontends together. They share the core, so
# installing one and forgetting the other leaves you running two different
# versions of the collector — which is exactly how a fixed bug appeared unfixed.
install-core:
	go build -o $(HOME)/.local/bin/wd ./cmd/wd
	go build -o $(HOME)/.local/bin/wdt ./cmd/wdt

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

check: fmt test vet build

run:
	go run ./cmd/wdt

clean:
	rm -f wd wdt wd-host build/workdash-firefox.xpi
