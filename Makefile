.PHONY: all build install test clean

all: build

build:
	bun build --compile --target=bun ./bin/zeuf.ts --outfile zeuf

install: build
	install -Dm755 zeuf $(HOME)/.local/bin/zeuf
	install -Dm755 zeuf $(HOME)/bin/zeuf 2>/dev/null || true

test:
	bun test

clean:
	rm -f zeuf
