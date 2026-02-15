build:
	@go build -o note-maker .

run: build
	./note-maker
