// Theme toggle, scroll-spy, verdict tabs, copy buttons. No dependencies.
(function () {
  var root = document.documentElement;
  var KEY = (window.__kaviraTheme || {}).key || "kavira-site-theme";
  var btn = document.getElementById("theme");

  function paint(theme) {
    root.setAttribute("data-theme", theme);
    var m = document.querySelector('meta[name="theme-color"]');
    if (m) m.setAttribute("content", theme === "dark" ? "#000000" : "#ffffff");
    if (btn) {
      btn.setAttribute("aria-pressed", theme === "dark" ? "true" : "false");
      btn.setAttribute("aria-label", theme === "dark" ? "Switch to light mode" : "Switch to dark mode");
    }
  }
  paint(root.getAttribute("data-theme") || "light");
  if (btn) btn.addEventListener("click", function () {
    var next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
    try { localStorage.setItem(KEY, next); } catch (e) { /* private mode */ }
    paint(next);
  });

  // Scroll-spy: mark the section nav link whose section is in view.
  var links = [].slice.call(document.querySelectorAll("#sections a"));
  if ("IntersectionObserver" in window && links.length) {
    var byId = {};
    links.forEach(function (a) { byId[a.getAttribute("href").slice(1)] = a; });
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (!e.isIntersecting) return;
        links.forEach(function (a) { a.removeAttribute("aria-current"); });
        var a = byId[e.target.id]; if (a) a.setAttribute("aria-current", "true");
      });
    }, { rootMargin: "-40% 0px -55% 0px" });
    Object.keys(byId).forEach(function (id) { var s = document.getElementById(id); if (s) io.observe(s); });
  }

  // Verdict tabs (WAI-ARIA tabs pattern: arrow keys, Home/End, roving tabindex).
  var tabs = [].slice.call(document.querySelectorAll('[role="tab"]'));
  function select(tab, focus) {
    tabs.forEach(function (t) {
      var on = t === tab;
      t.setAttribute("aria-selected", on ? "true" : "false");
      t.tabIndex = on ? 0 : -1;
      document.getElementById(t.getAttribute("aria-controls")).hidden = !on;
    });
    if (focus) tab.focus();
  }
  tabs.forEach(function (t, i) {
    t.addEventListener("click", function () { select(t, false); });
    t.addEventListener("keydown", function (e) {
      var n = { ArrowRight: i + 1, ArrowLeft: i - 1, Home: 0, End: tabs.length - 1 }[e.key];
      if (n === undefined) return;
      e.preventDefault();
      select(tabs[(n + tabs.length) % tabs.length], true);
    });
  });

  // Copy buttons. navigator.clipboard needs https/localhost; fall back so it also works elsewhere.
  var toast = document.getElementById("toast"), timer;
  function say(msg) {
    toast.textContent = msg; toast.hidden = false;
    clearTimeout(timer); timer = setTimeout(function () { toast.hidden = true; }, 2200);
  }
  function legacy(text) {
    var ta = document.createElement("textarea");
    ta.value = text; ta.setAttribute("readonly", ""); ta.style.position = "fixed"; ta.style.opacity = "0";
    document.body.appendChild(ta); ta.select();
    var ok = document.execCommand("copy"); document.body.removeChild(ta);
    if (!ok) throw new Error("copy refused");
  }
  [].forEach.call(document.querySelectorAll(".copy"), function (b) {
    b.addEventListener("click", function () {
      var text = b.parentNode.querySelector("pre").textContent;
      var done = function () { say("Copied to clipboard"); };
      var fail = function () { say("Copy failed. Select the text and copy it."); };
      if (navigator.clipboard && window.isSecureContext) navigator.clipboard.writeText(text).then(done, function () { try { legacy(text); done(); } catch (e) { fail(); } });
      else { try { legacy(text); done(); } catch (e) { fail(); } }
    });
  });
})();
