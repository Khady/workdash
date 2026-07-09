BINARY := workdash
NATIVE_HOST := workdash-native-host
CMD := ./cmd/workdash

.PHONY: all build extension install-firefox uninstall-firefox test vet fmt check run clean

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
