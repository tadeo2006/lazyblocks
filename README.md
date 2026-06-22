# LazyBlocks

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![Docker Engine](https://img.shields.io/badge/Docker-Required-2496ED?style=flat-square&logo=docker)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg?style=flat-square)](https://opensource.org/licenses/MIT)
[![TUI](https://img.shields.io/badge/Interface-TUI-8A2BE2?style=flat-square)](https://github.com/jesseduffield/gocui)

**LazyBlocks** is an extremely agile, visual, and professional Minecraft server manager designed exclusively to live inside your terminal. Inspired by top ecosystem tools like *LazyDocker* or *LazyGit*, **LazyBlocks** takes the technical nightmare of managing local servers (networking, ports, RAM, files, backups) and transforms it into a fluid, beautiful, 100% keyboard-navigable experience.

## Key Features

- **Quick Creation Wizard:** Never touch the terminal again. Create new servers and assign names and RAM limits with a single button. The manager automatically downloads the official image.
- **Native Modrinth Support (.mrpack):** Natively integrate and install Modrinth modpacks blazingly fast to boot up pre-loaded servers instantly.
- **Integrated Console & Quick Actions:** All logs live inside a beautiful real-time panel. You can also manage your worlds from the unified Server Actions menu (Start, Stop, Restart, etc).
- **File Explorer (Files):** Forget hunting for paths on your machine. Use the built-in native TUI explorer to navigate through your plugins, worlds, and mods.
- **Configuration Editor (Config):** Edit critical parameters of your server.properties directly from the terminal with dynamic auto-saving (Difficulty, Max Players, PvP, etc).
- **RCON Player Manager:** Extract information and monitor your online users in real-time through the Players tab (Via RCON protocol).
- **Automated Backups (Schedule):** Define max retention policies and schedule your machine's cron system to perform automated background backups, whether the panel is open or not.
- **Premium Design:** ANSI visual contrast, reactive borders that glow bright green upon entry, dynamic tabs, pop-up modals, and clean panel separation.

## System Requirements

- **Go** >= 1.22
- Running **Docker Engine** (Instances are automatically encapsulated via docker containers based on itzg/minecraft-server).
- UNIX or Linux environment with **cron** enabled (for the scheduled backups section).

## Global Installation (Recommended)

You can install LazyBlocks globally so it can be opened from anywhere in your terminal just by typing `lazyblocks`.

```bash
go install github.com/tadeo2006/lazyblocks/cmd/lazyblocks@latest
```

Ensure your `$(go env GOPATH)/bin` is in your system's `$PATH`.
Once installed, simply type in your terminal:
```bash
lazyblocks
```

## Local Development

1. **Clone the project:**
   ```bash
   git clone https://github.com/tadeo2006/lazyblocks.git
   cd lazyblocks
   ```

2. **Start the TUI:**
   Ensure the Docker daemon is alive and run:
   ```bash
   go run ./cmd/lazyblocks
   ```

## Key Controls (TUI Navigation)

- `<Tab>`: Cycle focus between the 3 main panels (Server Actions, Instances, Tab Panel).
- `Up / Down` or `k / j`: Navigate up or down through menus, file explorer, or configuration lists.
- `<Enter>`: Execute command / Enter folder (Files) / Toggle booleans (Config).
- `<Esc>`: Go back, cancel, or close pop-up modal windows.
- `Left / Right`: Quickly switch between active workspace tabs (Console, Players, Schedule, Files, Config).

## Architecture

LazyBlocks proudly stands on the solid graphical library [jesseduffield/gocui](https://github.com/jesseduffield/gocui) (the same one used by the legendary *lazygit*). 
The underlying management is orchestrated by asynchronously calling the official Docker SDK for Go, while worlds and master configurations are safely and persistently stored in `~/lazyblocks_data/`.

## License
This project is Open Source under the MIT License. Feel free to modify, learn, or contribute to make server management a fun, headache-free task.
