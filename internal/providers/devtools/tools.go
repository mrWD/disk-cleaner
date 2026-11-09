package devtools

// tool — описание одного инструмента разработки: как узнать, что он
// установлен и запущен, и где лежат его файлы. Все пути — от домашней папки.
//
// Чтобы раздел узнавал новый инструмент, достаточно добавить сюда запись.
type tool struct {
	name    string   // как показать: "Cursor"
	apps    []string // имена .app в /Applications — по ним видно «установлен» и «запущен»
	clis    []string // команды в терминале — по ним видно «установлен»
	markers []string // ещё признаки работы в командной строке процессов

	// data — всё, что принадлежит программе. Показывается целиком,
	// только когда программа удалена («остатки удалённых программ»).
	data []string

	extDir   string     // папка расширений редактора на основе VS Code
	support  string     // Application Support приложения на Electron: кэши внутри
	caches   []string   // кэши и временные файлы — пока программа работает, не трогаем
	runtimes []string   // папки, где каждая подпапка — скачанная среда выполнения
	versions *versionsT // папка с версиями CLI
	info     []infoT    // только для информации: размер и где чистить правильно
}

// versionsT — где CLI хранит свои версии: каждая версия — файл или папка
// в dir, а link — ссылка на текущую (её не трогаем).
type versionsT struct {
	dir, link string
}

// infoT — что показываем только для информации.
type infoT struct {
	path   string
	reason string // ключ перевода: где это чистить правильно
}

// Пути внутри ~/Library, которые встречаются часто.
const (
	support = "Library/Application Support/"
	caches  = "Library/Caches/"
)

// tools — все инструменты, которые знает раздел. Порядок — порядок строк.
var tools = []tool{
	{
		name: "Claude",
		apps: []string{"Claude"},
		// Данные приложения Claude трогать по частям нельзя: можно потерять
		// сессии. Только показываем, сколько занимают, и где их чистить.
		info: []infoT{{support + "Claude", reasonClaudeApp}},
	},
	{
		name: "Claude Code",
		clis: []string{"claude"},
		// Claude Code запускается и из терминала, и из приложения Claude.
		markers:  []string{"/.local/share/claude/versions/", "/Claude/claude-code/"},
		versions: &versionsT{dir: ".local/share/claude/versions", link: ".local/bin/claude"},
		caches:   []string{caches + "claude-cli-nodejs"},
		// ~/.claude — настройки и история; его использует и Claude в приложении,
		// поэтому он никогда не считается «остатками».
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

// electronCaches — папки внутри Application Support приложений на Electron
// (VS Code, Cursor, Orca…), которые приложение пересоздаёт само. User/ и
// workspaceStorage сюда не входят: там настройки, состояние проектов и
// (в Cursor) история чатов.
var electronCaches = []string{
	"Cache", "CachedData", "Code Cache", "GPUCache", "DawnGraphiteCache",
	"DawnWebGPUCache", "logs", "CachedExtensionVSIXs",
}

// product — IDE, которая хранит настройки отдельно для каждой версии:
// ~/Library/Application Support/JetBrains/IntelliJIdea2024.2 и т.п.
type product struct {
	name   string   // "IntelliJ IDEA"
	prefix string   // начало имени папки: "IntelliJIdea"
	apps   []string // варианты имени .app
}

// versioned — где такие IDE держат папки версий. У JetBrains — в трёх
// местах (настройки, кэши, логи), у Android Studio — только настройки
// (её кэши лежат внутри ~/Library/Caches/Google вместе с кэшем Chrome,
// а тот относится к разделу «Кэши приложений»).
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
