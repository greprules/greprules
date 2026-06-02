.PHONY: test build vet clean

test:
	go test ./...

vet:
	go vet ./...

build:
	go build -o greprules ./cmd/greprules

clean:
	rm -f greprules coverage.out
	rm -rf dist

