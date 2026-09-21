(function () {
  var buttons = document.querySelectorAll(".copy[data-copy]");
  for (var i = 0; i < buttons.length; i++) {
    buttons[i].addEventListener("click", onCopy);
  }

  function onCopy(ev) {
    var btn = ev.currentTarget;
    var text = btn.getAttribute("data-copy") || "";
    var done = btn.getAttribute("data-done") || "copied";
    var label = btn.getAttribute("data-label") || btn.textContent;

    function ok() {
      btn.textContent = done;
      btn.classList.add("is-done");
      window.setTimeout(function () {
        btn.textContent = label;
        btn.classList.remove("is-done");
      }, 1600);
    }

    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(ok).catch(function () {
        fallback(text, ok);
      });
      return;
    }
    fallback(text, ok);
  }

  function fallback(text, ok) {
    var ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.left = "-9999px";
    document.body.appendChild(ta);
    ta.select();
    try {
      document.execCommand("copy");
      ok();
    } finally {
      document.body.removeChild(ta);
    }
  }
})();
