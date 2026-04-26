BINARY := workdash
CMD := ./cmd/workdash

.PHONY: all build test vet fmt check run clean

all: build

build:
	go build -o $(BINARY) $(CMD)

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
	rm -f $(BINARY)
