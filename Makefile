run: build
	@./bin/vault-engineer

build:
	go build -o bin/vault-engineer
