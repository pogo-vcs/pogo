<div align="center">
<h1 align="center">
<img src="/brand/logo.svg" width="150" alt="P" />
<br>
Pogo
</h1>
<p align="center">
<strong>A centralized version control system that is simple and easy to use.</strong>
</p>
<p align="center">
<a href="https://github.com/pogo-vcs/pogo/releases"><img src="https://img.shields.io/github/v/release/pogo-vcs/pogo" alt="Latest release" /></a>
<a href="https://github.com/pogo-vcs/pogo/actions/workflows/test.yaml"><img src="https://img.shields.io/github/actions/workflow/status/pogo-vcs/pogo/test.yaml" alt="GitHub Workflow Status" /></a>
<a href="https://github.com/pogo-vcs/pogo/blob/main/LICENSE"><img src="https://img.shields.io/github/license/pogo-vcs/pogo" alt="License" /></a>
</p>
</div>

Pogo is a centralized version control system designed to be straightforward and efficient. It features an easy-to-use CLI client, a simple web UI, and robust support for both text and binary files. Pogo treats conflicts as first-class citizens, allowing them to be pushed to the remote to be resolved later.

## ✨ Goals

- **🏠 Centralized Server:** A single source of truth for all your data.
- **💻 Easy CLI Client:** No need for a complex GUI.
- **🌐 Simple Web UI:** For easy viewing of your repositories.
- **🔄 Cross-Platform Consistency:** Works the same on all major operating systems.
- **📄 Text & Binary File Support:** Handles all file types with ease.
- **💥 First-Class Conflicts:** Push conflicts to the remote and resolve them later.
- **🌳 No Named Branches:** Create branches by adding multiple children to a change and merge them by creating a new change with multiple parents. Changes are automaticall named.
- **🔖 Bookmarks:** Tag versions with bookmarks, like `main` for the current version or `v1.0.0` for a specific version. `main` is treated like a default branch in Git.
- **📦 Go Module Support:** Import a Pogo repository as a Go module, no additional configuration or software required.
- **🔒 Adaptive Security:** Automatically detects and uses HTTPS/TLS when available, gracefully falls back to HTTP when needed.

## 🚀 Installation

### NPM

```sh
npm install -g @pogo-vcs/pogo
```

### Homebrew

```sh
brew install --cask pogo-vcs/tap/pogo
```

### Scoop

```sh
scoop bucket add pogo-vcs https://github.com/pogo-vcs/scoop-bucket.git
scoop install pogo
```

### Docker (server)

```sh
docker pull ghcr.io/pogo-vcs/pogo:alpine
```

### From Source

To build Pogo from source, run the following commands:

```sh
git clone https://github.com/pogo-vcs/pogo.git
cd pogo
just build
```

This will create a `pogo` binary in the current directory. You can move this binary to a directory in your `PATH` to make it accessible from anywhere.

Required software for building:

- [go](https://go.dev/)
- [just](https://github.com/casey/just)
- [protoc](https://protobuf.dev/) (for gRPC)
- [sqlc](https://sqlc.dev/)
- [pnpm](https://pnpm.io/) (for Tailwind CSS)
- [templ](https://templ.guide/)

## 🕹️ Usage

The intended workflow for Pogo is simple and efficient:

1.  **describe your changes:** Before you start working, use the `pogo describe` command to write a detailed description of the changes you are about to make and why. This helps you to think about the changes and to communicate them to others.
2.  **Make your changes:** Make the changes to your files as you normally would.
3.  **Iterate on the description:** As you work, you can iterate on the description to reflect the changes you are making. Maybe your implementation plan changed and you need your description to reflect that.
4.  **Push your changes:** Regularly push your changes to the server using the `pogo push` command. A daemon process that pushes automatically will be added later. You constantly overwrite the current change until you are satisfied with it.
5.  **Create a new change:** When you are done with your changes, create a new one using the `pogo new` command. You can optionally add one or more parent changes to the command. By default, your current change is used as the parent.
6.  **Maintain a "main" bookmark:** Use bookmarks to tag important changes. You can set a bookmark with `pogo bookmark set main` to set the current change as the main one, or `pogo bookmark set main <change>` to set a specific change as main. "main" ist just a string, you can use any format for version bookmarks you want. But "main" is a special value, treated like a default branch in Git.

For running the server, you need to have a PostgreSQL database running and the following environment variables set:

- `DATABASE_URL`: The URL of the PostgreSQL database.
- `PUBLIC_ADDRESS`: The public address of the server.
- `PORT` or `HOST`: The port or host to listen on.
- `ROOT_TOKEN`: *optional* The root token for the server.
- `GC_MEMORY_THRESHOLD`: *optional* The number of files to use as the threshold for which garbage collection implementations will run (in memory vs batch processing).

## 📋 Commands

| Command         | Subcommand | Aliases            | Description                                                                                 |
| --------------- | ---------- | ------------------ | ------------------------------------------------------------------------------------------- |
| `pogo`          |            |                    | The root command for the Pogo CLI.                                                          |
| `pogo bookmark` |            | `b`                | Manage bookmarks.                                                                           |
|                 | `set`      | `s`                | Set a bookmark to a specific change. If no change is specified, the current change is used. |
|                 | `list`     | `l`                | List all bookmarks.                                                                         |
| `pogo commit`   |            |                    | Combines `describe`, `push`, and `new` into a single command.                               |
| `pogo describe` |            | `desc`, `rephrase` | Set the description for the current change.                                                 |
| `pogo edit`     |            | `checkout`         | Sets the specified revision as the working-copy revision.                                   |
| `pogo gc`       |            |                    | Run garbage collection on the server.                                                       |
| `pogo info`     |            |                    | Display the current working copy status.                                                    |
| `pogo init`     |            |                    | Initialize a new repository.                                                                |
| `pogo log`      |            |                    | Show the change history.                                                                    |
| `pogo new`      |            |                    | Create a new change based on one or more parent changes.                                    |
| `pogo push`     |            |                    | Push a change to the repository.                                                            |
| `pogo rm`       |            |                    | Remove a change from the repository.                                                        |
| `pogo serve`    |            |                    | Start the Pogo server.                                                                      |
| `pogo whoami`   |            |                    | Show the personal access token being used for the current repository.                       |

## 🏗️ Architecture

Pogo uses a PostgreSQL server to store all metadata about repositories, changes, and files. The actual file contents are stored in an object store on the file system. This separation of metadata and content allows for efficient storage and retrieval of data.

## 🗑️ Garbage Collection

Pogo includes an automatic garbage collection system that removes unreachable data to prevent unbounded storage growth. The GC system cleans up both database records and filesystem objects that are no longer referenced by any repository.

### How to Use

- **Manual GC:** Run `pogo gc` from any repository to trigger garbage collection on the server. This requires authentication.
- **Automatic GC:** When running `pogo serve`, garbage collection automatically runs daily at 3:00 AM server time.

### Adaptive Implementation

The garbage collection system uses an adaptive strategy based on the total number of files in the database:

- **Small-scale (< 10 million files):** Uses an in-memory hash map strategy for fast O(1) lookups.
- **Large-scale (≥ 10 million files):** Uses a batch processing strategy that scales to billions of files with constant memory usage.

The threshold can be configured via the `GC_MEMORY_THRESHOLD` environment variable.

## 📜 License

This project is published under the [Zlib license](LICENSE).

In short:

- You can use Pogo for any purpose, for free, including commercial use, forever.
- You can create and distribute modified versions of Pogo, but you must not misrepresent the software's origin, you must clearly mark your changes, and you must retain the original license notice.
- I don't take any responsibility for any damages or losses that may occur as a result of using Pogo. If you encounter any issues, please report them to me.

If you make any modifications to Pogo, I would appreciate it if you shared them with me. I'm always interested in learning from others and improving my own work.
