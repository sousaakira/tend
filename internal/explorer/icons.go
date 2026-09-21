package explorer

import (
	"path/filepath"
	"strings"
)

// Icons beside the tree's entries, herdr-sidebar's two themes: a Nerd
// Font's glyphs, which need that font in the terminal and are one column
// wide, and emoji, which every terminal has and which take two. None, the
// default, leaves the tree as text: an icon theme the font cannot draw is
// a column of boxes.

// Icon themes.
const (
	IconsNone  = "none"
	IconsNerd  = "nerd"
	IconsEmoji = "emoji"
)

// iconKind is what an entry is, for choosing its icon.
type iconKind int

const (
	iconFile iconKind = iota
	iconFolder
	iconFolderOpen
	iconGo
	iconRust
	iconPython
	iconJS
	iconTS
	iconShell
	iconMarkdown
	iconJSON
	iconConfig
	iconHTML
	iconCSS
	iconImage
	iconLock
	iconGit
	iconDocker
	iconC
	iconJava
	iconRuby
	iconArchive
	iconText
	iconMake
	iconLicense
)

var extKind = map[string]iconKind{
	".go": iconGo, ".rs": iconRust, ".py": iconPython, ".pyi": iconPython,
	".js": iconJS, ".mjs": iconJS, ".cjs": iconJS, ".jsx": iconJS,
	".ts": iconTS, ".tsx": iconTS,
	".sh": iconShell, ".bash": iconShell, ".zsh": iconShell, ".fish": iconShell,
	".md": iconMarkdown, ".markdown": iconMarkdown,
	".json": iconJSON, ".toml": iconConfig, ".yaml": iconConfig, ".yml": iconConfig,
	".ini": iconConfig, ".conf": iconConfig, ".env": iconConfig,
	".html": iconHTML, ".htm": iconHTML, ".css": iconCSS, ".scss": iconCSS,
	".png": iconImage, ".jpg": iconImage, ".jpeg": iconImage, ".gif": iconImage,
	".svg": iconImage, ".webp": iconImage, ".ico": iconImage,
	".lock": iconLock, ".c": iconC, ".h": iconC, ".cpp": iconC, ".cc": iconC, ".hpp": iconC,
	".java": iconJava, ".kt": iconJava, ".rb": iconRuby,
	".zip": iconArchive, ".gz": iconArchive, ".tar": iconArchive, ".tgz": iconArchive, ".xz": iconArchive,
	".txt": iconText, ".log": iconText,
}

var nameKind = map[string]iconKind{
	".gitignore": iconGit, ".gitattributes": iconGit, ".gitmodules": iconGit,
	"Dockerfile": iconDocker, "docker-compose.yml": iconDocker, "compose.yaml": iconDocker,
	"Makefile": iconMake, "LICENSE": iconLicense, "NOTICE": iconLicense,
	"go.sum": iconLock, "Cargo.lock": iconLock, "package-lock.json": iconLock,
}

// The two themes' glyphs. The Nerd Font ones are its seti, devicons and
// Font Awesome codepoints, which have stayed put across Nerd Fonts 2 and 3.
var (
	nerdIcons = map[iconKind]string{
		iconFile: "", iconFolder: "", iconFolderOpen: "",
		iconGo: "", iconRust: "", iconPython: "", iconJS: "",
		iconTS: "", iconShell: "", iconMarkdown: "", iconJSON: "",
		iconConfig: "", iconHTML: "", iconCSS: "", iconImage: "",
		iconLock: "", iconGit: "", iconDocker: "", iconC: "",
		iconJava: "", iconRuby: "", iconArchive: "", iconText: "",
		iconMake: "", iconLicense: "",
	}
	emojiIcons = map[iconKind]string{
		iconFile: "📄", iconFolder: "📁", iconFolderOpen: "📂",
		iconGo: "🐹", iconRust: "🦀", iconPython: "🐍", iconJS: "📜", iconTS: "📘",
		iconShell: "🐚", iconMarkdown: "📝", iconJSON: "📋", iconConfig: "🔧",
		iconHTML: "🌐", iconCSS: "🎨", iconImage: "🎨", iconLock: "🔒", iconGit: "🌱",
		iconDocker: "🐳", iconC: "🔩", iconJava: "☕", iconRuby: "💎", iconArchive: "📦",
		iconText: "📄", iconMake: "🔨", iconLicense: "📜",
	}
)

// kindOf is what kind of entry a node is.
func kindOf(n *Node) iconKind {
	if n.Dir {
		if n.Expanded {
			return iconFolderOpen
		}
		return iconFolder
	}
	if k, ok := nameKind[n.Name]; ok {
		return k
	}
	if k, ok := extKind[strings.ToLower(filepath.Ext(n.Name))]; ok {
		return k
	}
	return iconFile
}

// iconFor is the icon to draw before an entry, with the space after it, or
// "" when icons are off.
func iconFor(n *Node, theme string) string {
	var set map[iconKind]string
	switch theme {
	case IconsNerd:
		set = nerdIcons
	case IconsEmoji:
		set = emojiIcons
	default:
		return ""
	}
	if icon, ok := set[kindOf(n)]; ok {
		return icon + " "
	}
	return set[iconFile] + " "
}
