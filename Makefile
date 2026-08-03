BINARY := workdash
NATIVE_HOST := workdash-native-host
CMD := ./cmd/workdash

.PHONY: all build extension install-firefox uninstall-firefox install-core test vet fmt check run clean

all: build

build:
	go build -o $(BINARY) $(CMD)
	go build -o $(NATIVE_HOST) ./cmd/workdash-native-host

extension:
	mkdir -p build
	rm -f build/workdash-firefox.xpi
	cd browser/firefox && zip -qr ../../build/workdash-firefox.xpi .

install-firefox: extension
	bash scripts/install-firefox.sh

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
	go run $(CMD)

clean:
	rm -f $(BINARY) $(NATIVE_HOST) build/workdash-firefox.xpi
