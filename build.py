#!/usr/bin/env python3
"""Generate the tend site in en, pt-BR, es, ja and zh-CN."""

from pathlib import Path

ROOT = Path(__file__).resolve().parent
INSTALL = "curl -fsSL https://sousaakira.github.io/tend/install.sh | sh"

# Keys shared by every locale. Pane mock stays English on purpose: it is what
# the program draws, not marketing copy.
LANGS = {
    "en": {
        "html_lang": "en",
        "dir": ".",
        "href": {
            "en": "./",
            "pt": "pt/",
            "es": "es/",
            "ja": "ja/",
            "zh": "zh/",
        },
        "title": "tend — terminal runtime for coding agents",
        "description": "tend keeps agent terminals running after you detach, and marks each pane working, blocked, or idle.",
        "nav_session": "session",
        "nav_keys": "keys",
        "nav_install": "install",
        "nav_source": "source",
        "os_tabs_label": "Operating system",
        "os_linux": "Linux",
        "os_macos": "macOS",
        "os_windows": "Windows",
        "detected_linux": "detected Linux",
        "detected_macos": "detected macOS",
        "detected_windows": "detected Windows",
        "download_prefix": "Download",
        "all_builds": "all builds",
        "windows_p1": "tend is Linux and macOS only. There is no Windows binary.",
        "windows_p2": "On Windows, run the Linux installer inside WSL, or clone and <code>make install</code> on a Unix machine.",
        "deck": "A terminal runtime for coding agents. The server stays up when you leave. Each pane is marked working, blocked, or idle.",
        "stage_label": "A tend session",
        "facts_h": "What stays when you detach",
        "fact1_t": "The programs",
        "fact1_d": "A session is a background server. <kbd>ctrl</kbd><kbd>b</kbd> then <kbd>d</kbd> leaves the agents running. Opening tend again attaches to the same panes.",
        "fact2_t": "The state",
        "fact2_d": "A pane is working, blocked, or idle. The mark is what the screen is showing — a spinner, a permission prompt, a shell waiting — not a guess about the process name.",
        "fact3_t": "The layout",
        "fact3_d": "A space holds tabs. A tab holds panes. Split beside or below, zoom one pane, scroll its history, jump to the agent that stopped.",
        "keys_h": "Prefix is ctrl+b",
        "keys_note": "<kbd>ctrl</kbd><kbd>b</kbd> <kbd>?</kbd> lists every key inside the session.",
        "k_split": "split beside, below",
        "k_focus": "move focus",
        "k_resize": "resize",
        "k_zoom": "zoom a pane to the window",
        "k_scroll": "scroll back through its history",
        "k_close": "close a pane",
        "k_tabs": "tabs: new, next, previous, by number",
        "k_spaces": "spaces: new, previous, next",
        "k_agents": "show every agent, grouped",
        "k_jump": "pick one and jump to it",
        "k_detach": "detach, leaving everything running",
        "install_h": "Install",
        "copy": "copy",
        "copied": "copied",
        "install_p1": "Puts a binary in <code>~/.local/bin</code> (or <code>$GOBIN</code> / <code>$TEND_INSTALL_DIR</code>). Prefers a release build for this machine; otherwise clones and builds with Go.",
        "install_note": "From a checkout: <code>make install</code>.",
        "install_p2": "With no arguments, tend opens a session and starts a server if there is not one. <code>tend new -- claude</code> opens a pane. <code>tend ls</code> lists what is running. <code>-s NAME</code> runs another session beside the first.",
        "install_p3": "Written in Go. One binary. No cgo.",
        "footer": "Apache-2.0. An independent Go implementation; see NOTICE in the repository.",
    },
    "pt": {
        "html_lang": "pt-BR",
        "dir": "pt",
        "href": {
            "en": "../",
            "pt": "./",
            "es": "../es/",
            "ja": "../ja/",
            "zh": "../zh/",
        },
        "title": "tend — runtime de terminal para agentes de código",
        "description": "O tend mantém os terminais dos agentes rodando depois que você sai, e marca cada painel como working, blocked ou idle.",
        "nav_session": "sessão",
        "nav_keys": "teclas",
        "nav_install": "instalar",
        "nav_source": "código",
        "os_tabs_label": "Sistema operacional",
        "os_linux": "Linux",
        "os_macos": "macOS",
        "os_windows": "Windows",
        "detected_linux": "detectado Linux",
        "detected_macos": "detectado macOS",
        "detected_windows": "detectado Windows",
        "download_prefix": "Baixar",
        "all_builds": "todos os builds",
        "windows_p1": "O tend é só Linux e macOS. Não há binário para Windows.",
        "windows_p2": "No Windows, rode o instalador Linux no WSL, ou clone e <code>make install</code> numa máquina Unix.",
        "deck": "Um runtime de terminal para agentes de código. O servidor continua quando você sai. Cada painel fica marcado como working, blocked ou idle.",
        "stage_label": "Uma sessão do tend",
        "facts_h": "O que fica quando você desconecta",
        "fact1_t": "Os programas",
        "fact1_d": "Uma sessão é um servidor em segundo plano. <kbd>ctrl</kbd><kbd>b</kbd> e depois <kbd>d</kbd> deixa os agentes rodando. Abrir o tend de novo volta aos mesmos painéis.",
        "fact2_t": "O estado",
        "fact2_d": "Um painel está working, blocked ou idle. A marca é o que a tela mostra — um spinner, um pedido de permissão, um shell à espera — não um chute pelo nome do processo.",
        "fact3_t": "O layout",
        "fact3_d": "Um space guarda abas. Uma aba guarda painéis. Divida ao lado ou abaixo, amplie um painel, role o histórico, salte para o agente que parou.",
        "keys_h": "O prefixo é ctrl+b",
        "keys_note": "<kbd>ctrl</kbd><kbd>b</kbd> <kbd>?</kbd> lista todas as teclas dentro da sessão.",
        "k_split": "dividir ao lado, abaixo",
        "k_focus": "mover o foco",
        "k_resize": "redimensionar",
        "k_zoom": "ampliar um painel na janela",
        "k_scroll": "voltar no histórico",
        "k_close": "fechar um painel",
        "k_tabs": "abas: nova, seguinte, anterior, por número",
        "k_spaces": "spaces: novo, anterior, seguinte",
        "k_agents": "mostrar todos os agentes, agrupados",
        "k_jump": "escolher um e ir até ele",
        "k_detach": "desconectar, deixando tudo rodando",
        "install_h": "Instalar",
        "copy": "copiar",
        "copied": "copiado",
        "install_p1": "Coloca o binário em <code>~/.local/bin</code> (ou <code>$GOBIN</code> / <code>$TEND_INSTALL_DIR</code>). Prefere um release para esta máquina; senão clona e compila com Go.",
        "install_note": "A partir de um checkout: <code>make install</code>.",
        "install_p2": "Sem argumentos, o tend abre uma sessão e sobe um servidor se ainda não houver. <code>tend new -- claude</code> abre um painel. <code>tend ls</code> lista o que está rodando. <code>-s NAME</code> roda outra sessão ao lado da primeira.",
        "install_p3": "Escrito em Go. Um binário. Sem cgo.",
        "footer": "Apache-2.0. Implementação independente em Go; veja NOTICE no repositório.",
    },
    "es": {
        "html_lang": "es",
        "dir": "es",
        "href": {
            "en": "../",
            "pt": "../pt/",
            "es": "./",
            "ja": "../ja/",
            "zh": "../zh/",
        },
        "title": "tend — runtime de terminal para agentes de código",
        "description": "tend mantiene los terminales de los agentes tras desconectarte, y marca cada panel como working, blocked o idle.",
        "nav_session": "sesión",
        "nav_keys": "teclas",
        "nav_install": "instalar",
        "nav_source": "código",
        "os_tabs_label": "Sistema operativo",
        "os_linux": "Linux",
        "os_macos": "macOS",
        "os_windows": "Windows",
        "detected_linux": "detectado Linux",
        "detected_macos": "detectado macOS",
        "detected_windows": "detectado Windows",
        "download_prefix": "Descargar",
        "all_builds": "todas las builds",
        "windows_p1": "tend es solo Linux y macOS. No hay binario para Windows.",
        "windows_p2": "En Windows, ejecuta el instalador Linux en WSL, o clona y <code>make install</code> en una máquina Unix.",
        "deck": "Un runtime de terminal para agentes de código. El servidor sigue cuando te vas. Cada panel queda marcado como working, blocked o idle.",
        "stage_label": "Una sesión de tend",
        "facts_h": "Qué queda al desconectarte",
        "fact1_t": "Los programas",
        "fact1_d": "Una sesión es un servidor en segundo plano. <kbd>ctrl</kbd><kbd>b</kbd> y luego <kbd>d</kbd> deja los agentes en marcha. Abrir tend otra vez vuelve a los mismos paneles.",
        "fact2_t": "El estado",
        "fact2_d": "Un panel está working, blocked o idle. La marca es lo que muestra la pantalla — un spinner, un permiso, un shell a la espera — no una conjetura por el nombre del proceso.",
        "fact3_t": "El diseño",
        "fact3_d": "Un space guarda pestañas. Una pestaña guarda paneles. Divide al lado o abajo, amplía un panel, recorre el historial, salta al agente que se detuvo.",
        "keys_h": "El prefijo es ctrl+b",
        "keys_note": "<kbd>ctrl</kbd><kbd>b</kbd> <kbd>?</kbd> lista todas las teclas dentro de la sesión.",
        "k_split": "dividir al lado, abajo",
        "k_focus": "mover el foco",
        "k_resize": "redimensionar",
        "k_zoom": "ampliar un panel a la ventana",
        "k_scroll": "volver en el historial",
        "k_close": "cerrar un panel",
        "k_tabs": "pestañas: nueva, siguiente, anterior, por número",
        "k_spaces": "spaces: nuevo, anterior, siguiente",
        "k_agents": "mostrar todos los agentes, agrupados",
        "k_jump": "elegir uno e ir a él",
        "k_detach": "desconectar, dejando todo en marcha",
        "install_h": "Instalar",
        "copy": "copiar",
        "copied": "copiado",
        "install_p1": "Deja el binario en <code>~/.local/bin</code> (o <code>$GOBIN</code> / <code>$TEND_INSTALL_DIR</code>). Prefiere un release para esta máquina; si no, clona y compila con Go.",
        "install_note": "Desde un checkout: <code>make install</code>.",
        "install_p2": "Sin argumentos, tend abre una sesión y arranca un servidor si no hay uno. <code>tend new -- claude</code> abre un panel. <code>tend ls</code> lista lo que corre. <code>-s NAME</code> abre otra sesión junto a la primera.",
        "install_p3": "Escrito en Go. Un binario. Sin cgo.",
        "footer": "Apache-2.0. Implementación independiente en Go; ver NOTICE en el repositorio.",
    },
    "ja": {
        "html_lang": "ja",
        "dir": "ja",
        "href": {
            "en": "../",
            "pt": "../pt/",
            "es": "../es/",
            "ja": "./",
            "zh": "../zh/",
        },
        "title": "tend — コーディングエージェント向けターミナルランタイム",
        "description": "tend はデタッチ後もエージェントのターミナルを動かし続け、各ペインを working / blocked / idle と表示します。",
        "nav_session": "セッション",
        "nav_keys": "キー",
        "nav_install": "インストール",
        "nav_source": "ソース",
        "os_tabs_label": "OS",
        "os_linux": "Linux",
        "os_macos": "macOS",
        "os_windows": "Windows",
        "detected_linux": "Linux を検出",
        "detected_macos": "macOS を検出",
        "detected_windows": "Windows を検出",
        "download_prefix": "ダウンロード",
        "all_builds": "すべてのビルド",
        "windows_p1": "tend は Linux と macOS のみです。Windows 用のバイナリはありません。",
        "windows_p2": "Windows では WSL 上で Linux 用インストーラを使うか、Unix でクローンして <code>make install</code> してください。",
        "deck": "コーディングエージェント向けのターミナルランタイム。離れてもサーバーは動き続け、各ペインは working・blocked・idle と示されます。",
        "stage_label": "tend のセッション",
        "facts_h": "デタッチしても残るもの",
        "fact1_t": "プログラム",
        "fact1_d": "セッションはバックグラウンドのサーバーです。<kbd>ctrl</kbd><kbd>b</kbd> のあと <kbd>d</kbd> でエージェントは動き続けます。tend を開き直すと同じペインに戻ります。",
        "fact2_t": "状態",
        "fact2_d": "ペインは working・blocked・idle のいずれかです。印は画面が示しているもの — スピナー、許可プロンプト、待機中のシェル — であり、プロセス名からの推測ではありません。",
        "fact3_t": "レイアウト",
        "fact3_d": "space はタブを持ち、タブはペインを持ちます。横や下に分割し、ズームし、履歴をスクロールし、止まったエージェントへジャンプします。",
        "keys_h": "プレフィックスは ctrl+b",
        "keys_note": "<kbd>ctrl</kbd><kbd>b</kbd> <kbd>?</kbd> でセッション内の全キーを一覧します。",
        "k_split": "横・下に分割",
        "k_focus": "フォーカス移動",
        "k_resize": "リサイズ",
        "k_zoom": "ペインをウィンドウいっぱいに",
        "k_scroll": "履歴をスクロール",
        "k_close": "ペインを閉じる",
        "k_tabs": "タブ: 新規・次・前・番号",
        "k_spaces": "space: 新規・前・次",
        "k_agents": "エージェント一覧（グループ表示）",
        "k_jump": "選んでジャンプ",
        "k_detach": "デタッチ（実行はそのまま）",
        "install_h": "インストール",
        "copy": "コピー",
        "copied": "コピー済み",
        "install_p1": "バイナリを <code>~/.local/bin</code>（または <code>$GOBIN</code> / <code>$TEND_INSTALL_DIR</code>）に置きます。このマシン向けのリリースがあればそれを使い、なければ Go でクローンしてビルドします。",
        "install_note": "チェックアウトから: <code>make install</code>。",
        "install_p2": "引数なしで tend を実行するとセッションを開き、サーバーがなければ起動します。<code>tend new -- claude</code> でペインを開き、<code>tend ls</code> で一覧、<code>-s NAME</code> で別セッションを並べます。",
        "install_p3": "Go 製。単一バイナリ。cgo なし。",
        "footer": "Apache-2.0。独立した Go 実装。詳細はリポジトリの NOTICE を参照。",
    },
    "zh": {
        "html_lang": "zh-CN",
        "dir": "zh",
        "href": {
            "en": "../",
            "pt": "../pt/",
            "es": "../es/",
            "ja": "../ja/",
            "zh": "./",
        },
        "title": "tend — 面向编程智能体的终端运行时",
        "description": "tend 在你离开后仍保持智能体终端运行，并为每个窗格标出 working、blocked 或 idle。",
        "nav_session": "会话",
        "nav_keys": "快捷键",
        "nav_install": "安装",
        "nav_source": "源码",
        "os_tabs_label": "操作系统",
        "os_linux": "Linux",
        "os_macos": "macOS",
        "os_windows": "Windows",
        "detected_linux": "已检测 Linux",
        "detected_macos": "已检测 macOS",
        "detected_windows": "已检测 Windows",
        "download_prefix": "下载",
        "all_builds": "全部构建",
        "windows_p1": "tend 仅支持 Linux 和 macOS。没有 Windows 二进制文件。",
        "windows_p2": "在 Windows 上，请在 WSL 中运行 Linux 安装命令，或在 Unix 机器上克隆后执行 <code>make install</code>。",
        "deck": "面向编程智能体的终端运行时。你离开后服务器仍在运行。每个窗格标为 working、blocked 或 idle。",
        "stage_label": "一个 tend 会话",
        "facts_h": "断开后仍会留下的",
        "fact1_t": "程序",
        "fact1_d": "会话是后台服务器。<kbd>ctrl</kbd><kbd>b</kbd> 再按 <kbd>d</kbd> 后智能体继续运行。再次打开 tend 会回到同样的窗格。",
        "fact2_t": "状态",
        "fact2_d": "窗格是 working、blocked 或 idle。标记来自屏幕上的内容 — 转圈、权限提示、等待中的 shell — 不是根据进程名猜测。",
        "fact3_t": "布局",
        "fact3_d": "space 容纳标签。标签容纳窗格。左右或上下分割，放大一个窗格，滚动历史，跳到停下来的智能体。",
        "keys_h": "前缀是 ctrl+b",
        "keys_note": "<kbd>ctrl</kbd><kbd>b</kbd> <kbd>?</kbd> 列出会话内的全部快捷键。",
        "k_split": "左右、上下分割",
        "k_focus": "移动焦点",
        "k_resize": "调整大小",
        "k_zoom": "将窗格放大到窗口",
        "k_scroll": "回看历史",
        "k_close": "关闭窗格",
        "k_tabs": "标签：新建、下一个、上一个、按数字",
        "k_spaces": "space：新建、上一个、下一个",
        "k_agents": "显示全部智能体（分组）",
        "k_jump": "选一个并跳转",
        "k_detach": "断开，保持一切运行",
        "install_h": "安装",
        "copy": "复制",
        "copied": "已复制",
        "install_p1": "将二进制放到 <code>~/.local/bin</code>（或 <code>$GOBIN</code> / <code>$TEND_INSTALL_DIR</code>）。优先使用适合此机器的发布包；否则用 Go 克隆并编译。",
        "install_note": "从源码目录：<code>make install</code>。",
        "install_p2": "不带参数时，tend 打开会话，若没有服务器则启动一个。<code>tend new -- claude</code> 打开窗格。<code>tend ls</code> 列出正在运行的。<code>-s NAME</code> 在旁边再开一个会话。",
        "install_p3": "用 Go 写成。单一二进制。无 cgo。",
        "footer": "Apache-2.0。独立的 Go 实现；详见仓库中的 NOTICE。",
    },
}


# Runs in <head> so a first visit is redirected before the English page paints.
# The browser language is used only until the visitor picks one. That pick is
# written on the click itself (not in the deferred site.js), so a later load
# cannot bounce them back to the detected locale.
LANG_JS = """
(function(){
  if (location.protocol === 'file:') return;
  var KEY = 'tend-lang';
  var ok = {en:1, pt:1, es:1, ja:1, zh:1};

  function siteRoot() {
    var p = location.pathname.replace(/index\\.html$/, '');
    p = p.replace(/\\/(pt|es|ja|zh)\\/?$/, '/');
    if (p.slice(-1) !== '/') p += '/';
    return p;
  }

  function save(lang) {
    if (!ok[lang]) return;
    try { localStorage.setItem(KEY, lang); } catch (e) {}
    try {
      document.cookie = KEY + '=' + lang + '; path=' + siteRoot() + '; max-age=31536000; SameSite=Lax';
    } catch (e) {}
  }

  function read() {
    try {
      var v = localStorage.getItem(KEY);
      if (v && ok[v]) return v;
    } catch (e) {}
    try {
      var parts = document.cookie.split(';');
      for (var i = 0; i < parts.length; i++) {
        var s = parts[i].replace(/^\\s+/, '').split('=');
        if (s[0] === KEY && ok[s[1]]) return s[1];
      }
    } catch (e) {}
    return null;
  }

  document.addEventListener('click', function (ev) {
    var el = ev.target;
    while (el && el !== document) {
      if (el.getAttribute && el.getAttribute('data-lang') && el.className !== undefined) {
        var wrap = el.parentNode;
        if (wrap && wrap.className && (' ' + wrap.className + ' ').indexOf(' langs ') !== -1) {
          save(el.getAttribute('data-lang'));
          return;
        }
      }
      el = el.parentNode;
    }
  }, true);

  var path = location.pathname;
  var cur = 'en';
  var m = path.match(/\\/(pt|es|ja|zh)(?:\\/index\\.html|\\/)?$/);
  if (m) cur = m[1];
  var stored = read();
  function fromBrowser() {
    var list = navigator.languages || [navigator.language || navigator.userLanguage || ''];
    for (var i = 0; i < list.length; i++) {
      var c = String(list[i] || '').toLowerCase();
      if (c.indexOf('pt') === 0) return 'pt';
      if (c.indexOf('es') === 0) return 'es';
      if (c.indexOf('ja') === 0) return 'ja';
      if (c.indexOf('zh') === 0) return 'zh';
    }
    return 'en';
  }
  var want = stored || fromBrowser();
  if (want !== cur && (stored || cur === 'en')) {
    location.replace(want === 'en' ? siteRoot() : siteRoot() + want + '/');
  }
})();
""".strip()

BIN_LINUX = "https://github.com/sousaakira/tend/releases/latest/download/tend-linux-amd64"
RELEASES = "https://github.com/sousaakira/tend/releases/latest"


def page(code: str, t: dict) -> str:
    css = "site.css" if t["dir"] == "." else "../site.css"
    js = "site.js" if t["dir"] == "." else "../site.js"
    h = t["href"]

    def lang_link(key: str, label: str) -> str:
        current = ' aria-current="page"' if key == code else ""
        # Inline save so a language click is remembered even if site.js never runs.
        return (
            f'<a href="{h[key]}" data-lang="{key}"{current} '
            f"onclick=\"try{{localStorage.setItem('tend-lang','{key}')}}catch(e){{}}\">{label}</a>"
        )

    langs = " · ".join(
        [
            lang_link("pt", "pt"),
            lang_link("en", "en"),
            lang_link("es", "es"),
            lang_link("ja", "ja"),
            lang_link("zh", "zh"),
        ]
    )

    return f"""<!DOCTYPE html>
<html lang="{t["html_lang"]}" data-locale="{code}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{t["title"]}</title>
  <meta name="description" content="{t["description"]}">
  <link rel="alternate" hreflang="en" href="https://sousaakira.github.io/tend/">
  <link rel="alternate" hreflang="pt-BR" href="https://sousaakira.github.io/tend/pt/">
  <link rel="alternate" hreflang="es" href="https://sousaakira.github.io/tend/es/">
  <link rel="alternate" hreflang="ja" href="https://sousaakira.github.io/tend/ja/">
  <link rel="alternate" hreflang="zh-CN" href="https://sousaakira.github.io/tend/zh/">
  <link rel="alternate" hreflang="zh" href="https://sousaakira.github.io/tend/zh/">
  <link rel="alternate" hreflang="x-default" href="https://sousaakira.github.io/tend/">
  <link rel="stylesheet" href="{css}">
  <script>{LANG_JS}</script>
</head>
<body>
  <header class="bar">
    <a class="mark" href="#top">tend</a>
    <nav>
      <a href="#install">{t["nav_install"]}</a>
      <a href="#session">{t["nav_session"]}</a>
      <a href="#keys">{t["nav_keys"]}</a>
      <a href="https://github.com/sousaakira/tend">{t["nav_source"]}</a>
      <span class="langs">{langs}</span>
    </nav>
  </header>

  <main id="top">
    <section class="lead">
      <h1>tend</h1>
      <p class="deck">{t["deck"]}</p>
    </section>

    <section id="install"
      data-copy="{t["copy"]}"
      data-copied="{t["copied"]}"
      data-detected-linux="{t["detected_linux"]}"
      data-detected-macos="{t["detected_macos"]}"
      data-detected-windows="{t["detected_windows"]}"
      data-download="{t["download_prefix"]}">
      <h2>{t["install_h"]} <span class="os-detected" id="os-detected" hidden></span></h2>
      <div class="os-tabs" role="tablist" aria-label="{t["os_tabs_label"]}">
        <button type="button" role="tab" id="tab-linux" data-os="linux" aria-selected="true" aria-controls="panel-unix">{t["os_linux"]}</button>
        <button type="button" role="tab" id="tab-darwin" data-os="darwin" aria-selected="false" aria-controls="panel-unix">{t["os_macos"]}</button>
        <button type="button" role="tab" id="tab-windows" data-os="windows" aria-selected="false" aria-controls="panel-windows">{t["os_windows"]}</button>
      </div>
      <div id="panel-unix" class="install-panel" role="tabpanel" data-panel="unix">
        <div class="cmd-wrap">
          <pre class="cmd" id="install-cmd">{INSTALL}</pre>
          <button type="button" class="copy" data-copy="{INSTALL}" data-label="{t["copy"]}" data-done="{t["copied"]}">{t["copy"]}</button>
        </div>
        <p class="install-dl">
          <a id="bin-link" href="{BIN_LINUX}">{t["download_prefix"]} tend-linux-amd64</a>
          <span class="sep">·</span>
          <a id="bin-alt" href="https://github.com/sousaakira/tend/releases/latest/download/tend-linux-arm64">arm64</a>
          <span class="sep">·</span>
          <a href="{RELEASES}">{t["all_builds"]}</a>
        </p>
        <p>{t["install_p1"]}</p>
        <p class="note">{t["install_note"]}</p>
      </div>
      <div id="panel-windows" class="install-panel" role="tabpanel" data-panel="windows" hidden>
        <p>{t["windows_p1"]}</p>
        <p>{t["windows_p2"]}</p>
      </div>
    </section>

    <section id="session" class="stage" aria-label="{t["stage_label"]}">
      <div class="frame">
        <aside class="side">
          <p class="side-h">spaces</p>
          <p class="row on"><span class="dot"></span> main</p>
          <p class="row dim">master</p>
          <p class="rule" aria-hidden="true"></p>
          <p class="side-h">agents <span>flat</span></p>
          <p class="row on"><span class="dot work"></span> claude</p>
          <p class="row"><span class="dot block"></span> codex</p>
        </aside>
        <div class="panes">
          <article class="pane focus">
            <header><span class="id">1</span> claude <span class="st work" title="working">●</span></header>
            <pre>⠋ Pondering the refactor…

The panes belong to the server.
Closing this window does not
stop them.</pre>
          </article>
          <article class="pane">
            <header><span class="id">2</span> codex <span class="st block" title="blocked">▲</span></header>
            <pre>Do you want to proceed?

 ❯ 1. Yes
   2. No</pre>
          </article>
        </div>
      </div>
      <p class="status"><span>work · main · agents</span><span>1:claude ●</span><span>2:codex ▲</span></p>
    </section>

    <section class="usage">
      <p>{t["install_p2"]}</p>
      <p>{t["install_p3"]}</p>
    </section>

    <section class="facts">
      <h2>{t["facts_h"]}</h2>
      <dl>
        <div>
          <dt>{t["fact1_t"]}</dt>
          <dd>{t["fact1_d"]}</dd>
        </div>
        <div>
          <dt>{t["fact2_t"]}</dt>
          <dd>{t["fact2_d"]}</dd>
        </div>
        <div>
          <dt>{t["fact3_t"]}</dt>
          <dd>{t["fact3_d"]}</dd>
        </div>
      </dl>
    </section>

    <section id="keys">
      <h2>{t["keys_h"]}</h2>
      <p class="note">{t["keys_note"]}</p>
      <table>
        <tbody>
          <tr><th><kbd>|</kbd> <kbd>-</kbd></th><td>{t["k_split"]}</td></tr>
          <tr><th><kbd>h</kbd><kbd>j</kbd><kbd>k</kbd><kbd>l</kbd></th><td>{t["k_focus"]}</td></tr>
          <tr><th><kbd>H</kbd><kbd>J</kbd><kbd>K</kbd><kbd>L</kbd></th><td>{t["k_resize"]}</td></tr>
          <tr><th><kbd>z</kbd></th><td>{t["k_zoom"]}</td></tr>
          <tr><th><kbd>[</kbd></th><td>{t["k_scroll"]}</td></tr>
          <tr><th><kbd>x</kbd></th><td>{t["k_close"]}</td></tr>
          <tr><th><kbd>c</kbd> <kbd>n</kbd> <kbd>p</kbd> <kbd>1</kbd>–<kbd>9</kbd></th><td>{t["k_tabs"]}</td></tr>
          <tr><th><kbd>s</kbd> <kbd>(</kbd> <kbd>)</kbd></th><td>{t["k_spaces"]}</td></tr>
          <tr><th><kbd>a</kbd></th><td>{t["k_agents"]}</td></tr>
          <tr><th><kbd>g</kbd></th><td>{t["k_jump"]}</td></tr>
          <tr><th><kbd>d</kbd></th><td>{t["k_detach"]}</td></tr>
        </tbody>
      </table>
    </section>
  </main>

  <footer>
    <p>{t["footer"]}</p>
  </footer>
  <script src="{js}" defer></script>
</body>
</html>
"""


def main() -> None:
    for code, t in LANGS.items():
        html = page(code, t)
        if html.index('id="install"') > html.index('id="session"'):
            raise SystemExit("install section must sit above the session mock")
        if t["dir"] == ".":
            path = ROOT / "index.html"
        else:
            dest = ROOT / t["dir"]
            dest.mkdir(parents=True, exist_ok=True)
            path = dest / "index.html"
        path.write_text(html)
        print("wrote", path.relative_to(ROOT))


if __name__ == "__main__":
    main()
