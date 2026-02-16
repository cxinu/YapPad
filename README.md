# YapPad
A simple terminal-based note-taking app built with Go and Bubble Tea.

> Built while learning from a YouTube [tutorial](https://www.youtube.com/watch?v=pySTZsbaJ0Q) to explore TUI development with Bubble Tea.



https://github.com/user-attachments/assets/87614e6a-bad1-4fec-829e-2212d9d49c26



## Requirements
- Go 1.21+ (recommended)

## Installation

Clone the repository:
```bash
git clone https://github.com/A-Knee09/YapPad.git
cd YapPad
```

## Run with Go
```bash
go run main.go
```

## Run with Makefile
```bash
make run
```

## Build Binary
```bash 
make build
```
The compiled binary will be created in the project directory.

## Notes Storage
All notes are stored locally in:
```
$HOME/.YapPad
```
Each note is saved as a Markdown file inside that directory.

## Keyboard Shortcuts
- `ctrl+n` - Create new note
- `ctrl+l` - List all notes
- `ctrl+s` - Save current note
- `ctrl+d` - Delete selected note
- `ctrl+r` - Rename selected note
- `esc` - Go back/Close current view
- `ctrl+c` - Quit application
- `/` or start typing - Filter notes in list view
