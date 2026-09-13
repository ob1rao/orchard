.PHONY: build test bench release clean
build:
	go build -trimpath -o bin/orchard ./cmd/orchard
test: build
	go test -race ./...
	go vet ./...
	python3 scripts/test_installer.py
	python3 scripts/test_tui.py
bench:
	go test ./internal/scan ./internal/treemap -run '^$$' -bench . -benchmem
release:
	./scripts/build-release.sh $(VERSION)
clean:
	rm -rf bin dist
