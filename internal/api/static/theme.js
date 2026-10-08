// Runs in <head> before first paint. Kept as a file (not inline) so the CSP can stay script-src 'self'.
(function () {
  var KEY = "kavira-theme";
  var root = document.documentElement;
  var dark = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;

  function stored() {
    try {
      var v = localStorage.getItem(KEY);
      return v === "dark" || v === "light" ? v : null;
    } catch (e) {
      return null;
    }
  }
  function apply(theme) {
    root.setAttribute("data-theme", theme);
    var m = document.querySelector('meta[name="theme-color"]');
    if (m) m.setAttribute("content", theme === "dark" ? "#000000" : "#ffffff");
    var b = document.getElementById("theme");
    if (b) {
      b.setAttribute("aria-pressed", theme === "dark" ? "true" : "false");
      b.setAttribute("aria-label", theme === "dark" ? "Switch to light mode" : "Switch to dark mode");
    }
  }
  function current() {
    return stored() || (dark && dark.matches ? "dark" : "light");
  }

  window.KaviraTheme = {
    current: function () { return root.getAttribute("data-theme") || current(); },
    sync: function () { apply(root.getAttribute("data-theme") || current()); },
    toggle: function () {
      var next = this.current() === "dark" ? "light" : "dark";
      try { localStorage.setItem(KEY, next); } catch (e) { /* private mode */ }
      apply(next);
      return next;
    },
  };

  apply(current());
  // Follow the OS only while the user has not made an explicit choice.
  if (dark && dark.addEventListener) {
    dark.addEventListener("change", function () { if (!stored()) apply(current()); });
  }
})();
