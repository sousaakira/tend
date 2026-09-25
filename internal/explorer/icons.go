package explorer

import (
	"strings"

	"github.com/auth-com-br/tend/internal/vt"
)

// Icons beside the tree's entries are herdr-sidebar's (`icons.rs`), both
// themes: its "material" one — a Nerd Font's glyphs, each in the colour
// VS Code's Atom Material Icons gives the kind — and emoji, which every
// terminal has. The kinds, and which names and extensions are which, are
// its table as it is. None, the default, leaves the tree as text: an icon
// theme the font cannot draw is a column of boxes.

// Icon themes. "nerd" is herdr-sidebar's material theme.
const (
	IconsNone  = "none"
	IconsNerd  = "nerd"
	IconsEmoji = "emoji"
)

// iconKind is what an entry is, for choosing its icon.
type iconKind int

const (
	iconDir iconKind = iota
	iconDirOpen
	iconRust
	iconPython
	iconJs
	iconTs
	iconReact
	iconJson
	iconMarkdown
	iconHtml
	iconCss
	iconConfig
	iconXml
	iconShell
	iconPowerShell
	iconCFamily
	iconCSharp
	iconGo
	iconRuby
	iconPhp
	iconJava
	iconKotlin
	iconSwift
	iconLua
	iconSql
	iconData
	iconText
	iconLog
	iconPdf
	iconImage
	iconAudio
	iconVideo
	iconArchive
	iconLock
	iconBinary
	iconFont
	iconNotebook
	iconGit
	iconDocker
	iconPackage
	iconBuild
	iconReadme
	iconLicense
	iconEnvKey
	iconFile
)

// specialKind is a file known by its whole name, which beats its extension
// (Cargo.lock is a lock, not a lock-extension file; README.md is a readme).
func specialKind(lower string) (iconKind, bool) {
	switch lower {
	case "cargo.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml":
		return iconLock, true
	case "cargo.toml", "package.json", "pyproject.toml", "go.mod", "gemfile":
		return iconPackage, true
	case "makefile", "justfile", "cmakelists.txt":
		return iconBuild, true
	case ".gitignore", ".gitattributes", ".gitmodules":
		return iconGit, true
	}
	switch {
	case strings.HasPrefix(lower, "dockerfile"), strings.HasPrefix(lower, "docker-compose"):
		return iconDocker, true
	case strings.HasPrefix(lower, "readme"):
		return iconReadme, true
	case strings.HasPrefix(lower, "license"), lower == "copying":
		return iconLicense, true
	case lower == ".env", strings.HasPrefix(lower, ".env."):
		return iconEnvKey, true
	}
	return 0, false
}

var extKind = map[string]iconKind{
	"rs": iconRust,
	"py": iconPython, "pyi": iconPython,
	"js": iconJs, "mjs": iconJs, "cjs": iconJs,
	"ts":  iconTs,
	"jsx": iconReact, "tsx": iconReact,
	"json": iconJson, "jsonc": iconJson,
	"md": iconMarkdown, "markdown": iconMarkdown,
	"html": iconHtml, "htm": iconHtml,
	"css": iconCss, "scss": iconCss, "sass": iconCss, "less": iconCss,
	"toml": iconConfig, "yaml": iconConfig, "yml": iconConfig, "ini": iconConfig, "cfg": iconConfig, "conf": iconConfig,
	"xml": iconXml,
	"sh":  iconShell, "bash": iconShell, "zsh": iconShell, "fish": iconShell,
	"ps1": iconPowerShell, "psm1": iconPowerShell, "psd1": iconPowerShell, "bat": iconPowerShell, "cmd": iconPowerShell,
	"c": iconCFamily, "h": iconCFamily, "cpp": iconCFamily, "cc": iconCFamily, "cxx": iconCFamily, "hpp": iconCFamily, "hh": iconCFamily,
	"cs":   iconCSharp,
	"go":   iconGo,
	"rb":   iconRuby,
	"php":  iconPhp,
	"java": iconJava, "jar": iconJava,
	"kt": iconKotlin, "kts": iconKotlin,
	"swift": iconSwift,
	"lua":   iconLua,
	"sql":   iconSql, "db": iconSql, "sqlite": iconSql, "sqlite3": iconSql,
	"csv": iconData, "tsv": iconData,
	"txt": iconText,
	"log": iconLog,
	"pdf": iconPdf,
	"png": iconImage, "jpg": iconImage, "jpeg": iconImage, "gif": iconImage, "webp": iconImage, "bmp": iconImage, "ico": iconImage, "svg": iconImage, "tiff": iconImage,
	"mp3": iconAudio, "wav": iconAudio, "flac": iconAudio, "ogg": iconAudio,
	"mp4": iconVideo, "mkv": iconVideo, "avi": iconVideo, "mov": iconVideo, "webm": iconVideo,
	"zip": iconArchive, "tar": iconArchive, "gz": iconArchive, "tgz": iconArchive, "bz2": iconArchive, "xz": iconArchive, "7z": iconArchive, "rar": iconArchive,
	"lock": iconLock,
	"exe":  iconBinary, "dll": iconBinary, "so": iconBinary, "dylib": iconBinary, "a": iconBinary, "o": iconBinary, "bin": iconBinary, "wasm": iconBinary,
	"ttf": iconFont, "otf": iconFont, "woff": iconFont, "woff2": iconFont,
	"ipynb": iconNotebook,
}

// iconGlyph is one kind's icon in one theme: the glyph, and its colour
// (none for emoji, which bring their own).
type iconGlyph struct {
	glyph string
	color vt.Color
}

func rgb(hex uint32) vt.Color {
	return vt.RGBColor(uint8(hex>>16), uint8(hex>>8), uint8(hex))
}

// materialIcons are herdr-sidebar's material(): the glyph and the colour.
var materialIcons = map[iconKind]iconGlyph{
	iconDir:        {"\uf07b", rgb(0x90a4ae)},
	iconDirOpen:    {"\uf07c", rgb(0x90a4ae)},
	iconRust:       {"\ue7a8", rgb(0xdea584)},
	iconPython:     {"\ue73c", rgb(0x3572a5)},
	iconJs:         {"\ue74e", rgb(0xf1e05a)},
	iconTs:         {"\ue628", rgb(0x3178c6)},
	iconReact:      {"\ue7ba", rgb(0x61dafb)},
	iconJson:       {"\ue60b", rgb(0xcbcb41)},
	iconMarkdown:   {"\uf48a", rgb(0x519aba)},
	iconHtml:       {"\ue736", rgb(0xe34c26)},
	iconCss:        {"\ue749", rgb(0x42a5f5)},
	iconConfig:     {"\ue615", rgb(0x6d8086)},
	iconXml:        {"\uf121", rgb(0xe37933)},
	iconShell:      {"\uf489", rgb(0x4eaa25)},
	iconPowerShell: {"\U000f0a0a", rgb(0x5391fe)},
	iconCFamily:    {"\ue61d", rgb(0xf34b7d)},
	iconCSharp:     {"\U000f031b", rgb(0x178600)},
	iconGo:         {"\ue627", rgb(0x00add8)},
	iconRuby:       {"\ue791", rgb(0x701516)},
	iconPhp:        {"\ue73d", rgb(0x4f5d95)},
	iconJava:       {"\ue738", rgb(0xb07219)},
	iconKotlin:     {"\ue634", rgb(0xa97bff)},
	iconSwift:      {"\ue755", rgb(0xf05138)},
	iconLua:        {"\ue620", rgb(0x51a0cf)},
	iconSql:        {"\ue706", rgb(0xf29111)},
	iconData:       {"\uf1c3", rgb(0x33a852)},
	iconText:       {"\uf15c", rgb(0x9e9e9e)},
	iconLog:        {"\uf15c", rgb(0x757575)},
	iconPdf:        {"\uf1c1", rgb(0xe53935)},
	iconImage:      {"\uf1c5", rgb(0x26a69a)},
	iconAudio:      {"\uf1c7", rgb(0xec407a)},
	iconVideo:      {"\uf1c8", rgb(0xff7043)},
	iconArchive:    {"\uf1c6", rgb(0xafb42b)},
	iconLock:       {"\uf023", rgb(0xffd54f)},
	iconBinary:     {"\uf471", rgb(0xef5350)},
	iconFont:       {"\uf031", rgb(0xb0bec5)},
	iconNotebook:   {"\uf02d", rgb(0xf57c00)},
	iconGit:        {"\ue702", rgb(0xf14e32)},
	iconDocker:     {"\uf308", rgb(0x0db7ed)},
	iconPackage:    {"\uf487", rgb(0x8d6e63)},
	iconBuild:      {"\uf0ad", rgb(0x6d8086)},
	iconReadme:     {"\uf02d", rgb(0x42a5f5)},
	iconLicense:    {"\uf24e", rgb(0xffd54f)},
	iconEnvKey:     {"\uf084", rgb(0xffd54f)},
	iconFile:       {"\uf15b", rgb(0x90a4ae)},
}

// emojiIcons are herdr-sidebar's emoji(), none of them with a variation
// selector: their width differs between terminals and would skew the tree.
var emojiIcons = map[iconKind]string{
	iconDir:        "📁",
	iconDirOpen:    "📂",
	iconRust:       "🦀",
	iconPython:     "🐍",
	iconJs:         "🟨",
	iconTs:         "🔷",
	iconReact:      "🟦",
	iconJson:       "🧾",
	iconMarkdown:   "📝",
	iconHtml:       "🌐",
	iconCss:        "🎨",
	iconConfig:     "🔧",
	iconXml:        "📰",
	iconShell:      "🐚",
	iconPowerShell: "💻",
	iconCFamily:    "🔩",
	iconCSharp:     "🟣",
	iconGo:         "🐹",
	iconRuby:       "💎",
	iconPhp:        "🐘",
	iconJava:       "☕",
	iconKotlin:     "🟪",
	iconSwift:      "🐦",
	iconLua:        "🌙",
	iconSql:        "💾",
	iconData:       "📊",
	iconText:       "📄",
	iconLog:        "📋",
	iconPdf:        "📕",
	iconImage:      "📷",
	iconAudio:      "🎵",
	iconVideo:      "🎬",
	iconArchive:    "🧳",
	iconLock:       "🔒",
	iconBinary:     "⚡",
	iconFont:       "🔤",
	iconNotebook:   "📓",
	iconGit:        "🙈",
	iconDocker:     "🐳",
	iconPackage:    "📦",
	iconBuild:      "🔨",
	iconReadme:     "📖",
	iconLicense:    "📜",
	iconEnvKey:     "🔑",
	iconFile:       "📄",
}

// kindOf is what kind of entry a node is: herdr-sidebar's kind_of, the
// whole name before the extension, both without regard to case.
func kindOf(n *Node) iconKind {
	if n.Dir {
		if n.Expanded {
			return iconDirOpen
		}
		return iconDir
	}
	lower := strings.ToLower(n.Name)
	if k, ok := specialKind(lower); ok {
		return k
	}
	if i := strings.LastIndexByte(lower, '.'); i >= 0 {
		if k, ok := extKind[lower[i+1:]]; ok {
			return k
		}
	}
	return iconFile
}

// iconFor is the icon to draw before an entry, with the space after it, and
// the style to draw it in; "" when icons are off.
func iconFor(n *Node, theme string) (string, vt.Style) {
	switch theme {
	case IconsNerd:
		g := materialIcons[kindOf(n)]
		return g.glyph + " ", vt.Style{FG: g.color}
	case IconsEmoji:
		return emojiIcons[kindOf(n)] + " ", vt.Style{}
	}
	return "", vt.Style{}
}

// The views' icons on the activity bar, herdr-sidebar's activity_icons:
// Font Awesome's folder, magnifying glass and code fork with a Nerd Font,
// and their emoji.
var (
	nerdViewIcons  = [3]string{"\uf07b", "\uf002", "\uf126"}
	emojiViewIcons = [3]string{"📁", "🔍", "🔀"}
)

// viewIcon is a view's icon on the activity bar, or "" when icons are off.
func viewIcon(v View, theme string) string {
	switch theme {
	case IconsNerd:
		return nerdViewIcons[v]
	case IconsEmoji:
		return emojiViewIcons[v]
	}
	return ""
}
