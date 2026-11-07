.PHONY: build run

# build builds the command-line tool and the desktop app.
build:
	go build -o ralph .
	go build -o ralph-gui ./cmd/ralph-gui

# run builds the desktop app and starts it.
run:
	go build -o ralph-gui ./cmd/ralph-gui
	./ralph-gui
