.PHONY: deps generate clean

OUTPUT := ipmi-dashboard.json

deps:
	go mod download

generate: deps
	go run main.go $(OUTPUT)

clean:
	rm -f $(OUTPUT)
	go clean

check:
	go vet ./...
	go fmt ./...
