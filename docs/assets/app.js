// Highlighting, copy buttons, Code/Output tabs and the phone menu.
(function () {
  if (window.hljs) {
    document.querySelectorAll("pre code").forEach(function (el) {
      if (!el.classList.contains("language-text")) window.hljs.highlightElement(el);
    });
  }

  document.querySelectorAll("figure.code .copy").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var code = btn.closest("figure").querySelector("pre code").innerText;
      var done = function () {
        btn.textContent = "Copied";
        setTimeout(function () { btn.textContent = "Copy"; }, 1500);
      };
      if (navigator.clipboard) {
        navigator.clipboard.writeText(code).then(done, function () { btn.textContent = "Press ⌘C"; });
      }
    });
  });

  document.querySelectorAll(".example").forEach(function (ex) {
    var tabs = ex.querySelectorAll(".tab");
    tabs.forEach(function (tab) {
      tab.addEventListener("click", function () {
        tabs.forEach(function (t) { t.classList.toggle("active", t === tab); });
        ex.querySelectorAll(".panel").forEach(function (p) {
          p.hidden = p.getAttribute("data-panel") !== tab.getAttribute("data-tab");
        });
      });
    });
  });

  var menu = document.querySelector(".menu");
  if (menu) {
    menu.addEventListener("click", function () {
      var open = document.body.classList.toggle("nav-open");
      menu.setAttribute("aria-expanded", open ? "true" : "false");
    });
  }
})();
