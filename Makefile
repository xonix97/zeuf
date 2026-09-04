.PHONY: all build install test clean

all: build

build:
	go build -v -o zeuf main.go

install: build
	install -Dm755 zeuf $(HOME)/.local/bin/zeuf

test:
	go test -v ./...

clean:
	rm -f zeuf
