package devtools

// tool describes one development tool: how to tell that it is
// installed and running, and where its files are. All paths are relative to the home folder.
//
// For the section to recognize a new tool, it is enough to add an entry here.
type tool struct {
	name    string   // how to display it: "Cursor"
	apps    []string // names of .app in /Applications; they show "installed" and "running"
	clis    []string // commands in the terminal; they show "installed"
	markers []string // more signs of running, in the command lines of processes

	// data is everything that belongs to the program. Shown in full
	// only when the program is uninstalled ("leftovers of uninstalled apps").
	data []string

	extDir   string     // folder of extensions of an editor based on VS Code
	support  string     // Application Support of an Electron app: caches inside
	caches   []string   // caches and temporary files; not touched while the program runs
	runtimes []string   // folders where every subfolder is a downloaded runtime
	versions *versionsT // folder with CLI versions
	info     []infoT    // for information only: size and where to clean it properly
}

// versionsT is where a CLI keeps its versions: each version is a file or folder
// in dir, and link points to the current one (we do not touch it).
type versionsT struct {
	dir, link string
}

// infoT is what we show for information only.
type infoT struct {
	path   string
	reason string // translation key: where to clean this properly
}

// Paths inside ~/Library that come up often.
const (
	support = "Library/Application Support/"
	caches  = "Library/Caches/"
)

// tools are all the tools the section knows. The order is the order of the rows.
var tools = []tool{
	{
		name: "Claude",
		apps: []string{"Claude"},
		// We must not touch Claude app data piece by piece: we could lose
		// sessions. We only show how much space it takes and where to clean it.
		info: []infoT{{support + "Claude", reasonClaudeApp}},
	},
	{
		name: "Claude Code",
		clis: []string{"claude"},
		// Claude Code is launched both from the terminal and from the Claude app.
		markers:  []string{"/.local/share/claude/versions/", "/Claude/claude-code/"},
		versions: &versionsT{dir: ".local/share/claude/versions", link: ".local/bin/claude"},
		caches:   []string{caches + "claude-cli-nodejs"},
		// ~/.claude is settings and history; the Claude app uses it too,
		// so it never counts as "leftovers".
		data: []string{".local/share/claude", caches + "claude-cli-nodejs"},
		info: []infoT{{".claude/projects", reasonClaudeHistory}},
	},
	{
		name:     "Codex",
		apps:     []string{"ChatGPT", "Codex"},
		clis:     []string{"codex"},
		markers:  []string{"@openai/codex", "/bin/codex"},
		caches:   []string{".codex/.tmp", ".codex/cache", caches + "com.openai.codex"},
		runtimes: []string{".cache/codex-runtimes"},
		data:     []string{".codex", ".cache/codex-runtimes", support + "Codex", caches + "com.openai.codex"},
	},
	{
		name:    "Cursor",
		apps:    []string{"Cursor"},
		extDir:  ".cursor/extensions",
		support: support + "Cursor",
		caches:  []string{caches + "com.todesktop.230313mzl4w4u92"},
		data:    []string{".cursor", support + "Cursor", caches + "com.todesktop.230313mzl4w4u92"},
	},
	{
		name:     "Cursor CLI",
		clis:     []string{"cursor-agent"},
		versions: &versionsT{dir: ".local/share/cursor-agent/versions", link: ".local/bin/cursor-agent"},
		data:     []string{".local/share/cursor-agent"},
	},
	{
		name:    "VS Code",
		apps:    []string{"Visual Studio Code"},
		extDir:  ".vscode/extensions",
		support: support + "Code",
		data:    []string{".vscode", support + "Code", caches + "com.microsoft.VSCode"},
	},
	{
		name:    "VS Code Insiders",
		apps:    []string{"Visual Studio Code - Insiders"},
		extDir:  ".vscode-insiders/extensions",
		support: support + "Code - Insiders",
		data:    []string{".vscode-insiders", support + "Code - Insiders"},
	},
	{
		name:    "Windsurf",
		apps:    []string{"Windsurf"},
		extDir:  ".windsurf/extensions",
		support: support + "Windsurf",
		data:    []string{".windsurf", ".codeium", support + "Windsurf"},
	},
	{
		name:    "Antigravity",
		apps:    []string{"Antigravity"},
		clis:    []string{"antigravity"},
		extDir:  ".antigravity/extensions",
		support: support + "Antigravity",
		data:    []string{".antigravity", support + "Antigravity"},
	},
	{
		name:    "Kiro",
		apps:    []string{"Kiro"},
		extDir:  ".kiro/extensions",
		support: support + "Kiro",
		data:    []string{".kiro", support + "Kiro"},
	},
	{
		name:    "Trae",
		apps:    []string{"Trae"},
		extDir:  ".trae/extensions",
		support: support + "Trae",
		data:    []string{".trae", support + "Trae"},
	},
	{
		name:    "Void",
		apps:    []string{"Void"},
		extDir:  ".void-editor/extensions",
		support: support + "Void",
		data:    []string{".void-editor", support + "Void"},
	},
	{
		name:    "Orca",
		apps:    []string{"Orca"},
		support: support + "orca",
		data:    []string{".orca", support + "orca"},
	},
	{
		name: "Zed",
		apps: []string{"Zed"},
		data: []string{support + "Zed", caches + "dev.zed.Zed"},
	},
	{
		name: "Sublime Text",
		apps: []string{"Sublime Text"},
		data: []string{support + "Sublime Text"},
	},
	{
		name: "Warp",
		apps: []string{"Warp"},
		data: []string{support + "dev.warp.Warp-Stable"},
	},
	{
		name: "Visual Studio",
		apps: []string{"Visual Studio"},
		data: []string{support + "VisualStudio", caches + "VisualStudio", "Library/Logs/VisualStudio"},
	},
	{name: "Gemini CLI", clis: []string{"gemini"}, data: []string{".gemini"}},
	{name: "GitHub Copilot CLI", clis: []string{"copilot"}, data: []string{".copilot"}},
	{name: "Qwen Code", clis: []string{"qwen"}, data: []string{".qwen"}},
	{name: "opencode", clis: []string{"opencode"}, data: []string{".local/share/opencode", ".cache/opencode"}},
	{name: "Goose", clis: []string{"goose"}, data: []string{".config/goose", ".local/share/goose"}},
}

// electronCaches are folders inside Application Support of Electron apps
// (VS Code, Cursor, Orca…) that the app recreates itself. User/ and
// workspaceStorage are not included: they hold settings, project state and
// (in Cursor) chat history.
var electronCaches = []string{
	"Cache", "CachedData", "Code Cache", "GPUCache", "DawnGraphiteCache",
	"DawnWebGPUCache", "logs", "CachedExtensionVSIXs",
}

// product is an IDE that keeps settings separately for each version:
// ~/Library/Application Support/JetBrains/IntelliJIdea2024.2 and so on.
type product struct {
	name   string   // "IntelliJ IDEA"
	prefix string   // start of the folder name: "IntelliJIdea"
	apps   []string // variants of the .app name
}

// versioned is where such IDEs keep version folders. JetBrains uses three
// places (settings, caches, logs), Android Studio only settings
// (its caches live inside ~/Library/Caches/Google together with the Chrome cache,
// and that belongs to the "App caches & logs" section).
var versioned = []struct {
	dirs     []string
	products []product
}{
	{
		dirs: []string{support + "JetBrains", caches + "JetBrains", "Library/Logs/JetBrains"},
		products: []product{
			{"IntelliJ IDEA", "IntelliJIdea", []string{"IntelliJ IDEA", "IntelliJ IDEA Ultimate"}},
			{"IntelliJ IDEA CE", "IdeaIC", []string{"IntelliJ IDEA CE"}},
			{"WebStorm", "WebStorm", []string{"WebStorm"}},
			{"PyCharm", "PyCharm", []string{"PyCharm", "PyCharm Professional Edition"}},
			{"PyCharm CE", "PyCharmCE", []string{"PyCharm CE", "PyCharm Community Edition"}},
			{"GoLand", "GoLand", []string{"GoLand"}},
			{"Rider", "Rider", []string{"Rider"}},
			{"CLion", "CLion", []string{"CLion"}},
			{"PhpStorm", "PhpStorm", []string{"PhpStorm"}},
			{"RubyMine", "RubyMine", []string{"RubyMine"}},
			{"DataGrip", "DataGrip", []string{"DataGrip"}},
			{"RustRover", "RustRover", []string{"RustRover"}},
		},
	},
	{
		dirs:     []string{support + "Google"},
		products: []product{{"Android Studio", "AndroidStudio", []string{"Android Studio"}}},
	},
}
