# Disk Cleaner

A macOS app written in Go with [Fyne](https://fyne.io). It finds what takes up space and can be removed without losing anything, explains what each thing is and why it's safe to remove, and shows the exact list of commands before deleting anything.

| Section | What it finds |
|---|---|
| Worktrees | `node_modules`, `Pods`, `build`… in git worktrees (only what git ignores) |
| Projects | the same in your main repositories, with inactive ones (no commits for 30+ days) and active ones listed separately; plus "Leftover builds": `.xcarchive`, `.ipa`, `.apk`, `.aab`, `.dmg`, `.app` files that sit next to the code and aren't tracked by git (also in Worktrees) |
| Xcode | DerivedData, archives, device symbols, documentation cache, SwiftUI previews |
| Simulators | iOS/watchOS/tvOS/visionOS runtimes, the simulators themselves, dyld cache |
| Android | emulators (AVD), system images, NDK versions, SDK sources |
| Docker | containers, images, volumes, build cache |
| Developer tools | editors, IDEs and AI tools (Cursor, VS Code, Claude Code, Codex, JetBrains, Android Studio, Windsurf, Antigravity, Zed, Gemini CLI…; the full list is in `internal/providers/devtools/tools.go`): leftovers of uninstalled apps, old versions (CLIs, extensions, folders of previous IDE versions), caches and temp files, downloaded runtimes. Claude data and Claude Code history are shown for information only |
| AI models | LM Studio and Ollama models, Hugging Face cache, models downloaded by apps, the llmstore storage (shown only) |
| Package caches | npm and npx, Yarn, CocoaPods, Gradle (with downloaded JDK archives), uv, pip, Homebrew, Go, SwiftPM, browsers for tests, Electron, Expo, JS tools (with Prisma and Corepack), pre-commit, NuGet, Python bytecode |
| Downloads | installers (`.dmg`, `.pkg`), builds (`.aab`, `.apk`, `.ipa`, `.xcarchive`, `.app`) and archives (`.zip`, `.xip`, `.tar.gz`…) in Downloads. These files go to the Trash instead of being deleted; a mounted `.dmg` can't be selected |
| App caches & logs | app caches in `~/Library/Caches` (except Apple's system ones and Claude), update leftovers, `~/Library/Logs` |

## Install

macOS only.

```bash
git clone https://github.com/mrWD/disk-cleaner.git
cd disk-cleaner
./scripts/build.sh --install
```

The script installs whatever is missing for the build, builds the app, puts **Disk Cleaner** into `/Applications` and opens it. After that, launch it like any other app: from Launchpad, Spotlight or the Dock.

What the script installs if it isn't there yet:

- **Xcode Command Line Tools.** Fyne compiles part of itself from C, so a C compiler is needed. A macOS window will open: click Install and wait, the script continues on its own.
- **Go.** It uses Homebrew if you have it. Without Homebrew it downloads the official Go archive from go.dev into `~/sdk`, no administrator password needed. If you already have Go 1.21 or newer, it's kept: Go downloads the version this project needs by itself.
- **fyne**, the tool that packages the build into a `.app`.

If `git` isn't installed, macOS offers to install the Command Line Tools as soon as you run `git clone`. Accept, then run the commands again.

The first build takes a few minutes. To update later, run `git pull` and `./scripts/build.sh --install` again: the running copy is closed and replaced.

**Try it safely first:** turn on **Settings → Dry run**. The app then shows what it would delete, but doesn't touch anything.

The first time you open Downloads in the app, macOS asks for access to that folder. Because the app is signed ad-hoc (without a developer certificate), macOS may ask again after each rebuild.

## Development

```bash
go run .                      # run from source
go test ./...                 # all tests
./scripts/build.sh            # only build dist/Disk Cleaner.app (Spotlight will show it as a second copy)
go test -tags screenshots -run TestScreenshots ./internal/ui/ -args -lang=en -dark   # screenshots into screenshots/
```

With `go run .` the app runs from the Terminal: there's no Dock icon of its own, and macOS asks for Downloads access on behalf of the Terminal.

`go install github.com/mrWD/disk-cleaner@latest` doesn't work: the module is named `disk-cleaner`, not after its GitHub path.

Code comments are in Russian.

## Using it

- The **sidebar** lists the sections with how much each one found. At its bottom is the disk: free space and a bar like in macOS Storage, where each section's findings have the color of its icon. Under it are the **Rescan** and **Settings** buttons.
- **Clean All** under the disk cleans everything marked Safe in every section. **Clean All** next to a section's search does the same for that section only. Caution items and anything running or in use are left out: select those yourself. Both buttons first open the same review window with every command, nothing is deleted before you confirm.
- **Search everywhere** with the field in the sidebar (⌘F). Results appear on the right, grouped by section, and each section in the sidebar shows how much it matched.
- **Search in a section** with the field above its list.
- A query is split into words, and each word must appear in the name, path or category title: `lingary node` finds `node_modules` from the Lingary worktree.
- **Select All** selects what's visible right now (with a search, only the matches) and can be selected. Press it again to deselect. The checkbox in a category header selects the whole category.
- **Checkboxes belong to the section you're looking at.** When you open another section or start a search, they're cleared, so you never clean something you can't see.
- Everything is compact by default: one line per item (name, path, a Running/In use/Protected label, size). Click a category header to see what it is, why it's safe, what happens after cleaning, and full paths and reasons.

## What it never deletes

- Anything running (a container, simulator, emulator, the Gradle daemon, a process from a worktree): the checkbox is disabled and the reason is shown next to it.
- Simulator entries that share a disk image, the newest runtime of each platform, and simulators whose runtime is missing.
- Anything git tracks in a worktree.
- Your home folder, its top-level folders (`~/Projects`, `~/Library`…) and other apps' data (`~/Library/Application Support`, `Preferences`, `Keychains`, `Mail`…): the executor refuses even if a provider gets it wrong.
- With the administrator password, it only removes paths inside `/Library/Developer/CoreSimulator/Caches`. Commands with administrator rights are forbidden.

## Design

It follows Apple's apps: a sidebar with colored section icons (like System Settings), the disk bar (like Storage), grouped cards, Safe/Caution labels and one blue main button. Light and dark appearance: by default it follows macOS, and **Settings → Appearance** has three tiles, Light, Dark and Auto, like in System Settings.

The theme is in `internal/ui/theme.go`: macOS system colors and the San Francisco font. The font is read from the Mac itself (`/Library/Fonts/SF-Pro-Text-*.otf` or `/System/Library/Fonts/SFNS.ttf`) and isn't bundled, because Apple's license doesn't allow it. Without it, Fyne's own font is used.

A provider gives its section icon and color through the optional `core.Look` interface (`look.go` and `icon.svg` in the provider's folder).

## Architecture

```
main.go                      composition root: the list of providers
internal/
  core/                      the core: Item, Category, Selection, Plan, Provider, Executor (interfaces)
  diskutil/                  free space, folder sizes
  shell/                     running programs without a shell
  executor/                  the actual deletion + safety checks
  i18n/                      translations, language choice
  settings/                  settings (language, project folders, appearance, dry run)
  ui/                        the Fyne window; knows only core
  providers/
    kit/                     shared building blocks for providers
    providertest/            shared checks for provider tests
    gitscan/                 finding git repositories and ignored folders (for worktrees and projects)
    worktrees/ projects/ xcode/ simulators/ android/ docker/ models/ devtools/ packagecaches/ appcaches/ downloads/
```

Dependencies point to `core`: `core` imports nothing from the project, providers don't import each other, and `ui` doesn't know any provider.

### Adding a provider

1. A folder `internal/providers/<name>/` with a type that has the methods `ID`, `Categories`, `Scan`, `Plan`, `Translations` (see `internal/core/provider.go`).
2. Optionally `icon.svg` (a white 24×24 drawing) and `look.go` with the badge color.
3. `translations/en.json` and `ru.json`: `<name>.name` and, for each category, `title`, `what`, `why`, `after`.
4. A test with `providertest.CheckTranslations`: it checks that no text is missing.
5. One line in `main.go`.

To remove a provider, delete its folder and its line in `main.go`.
