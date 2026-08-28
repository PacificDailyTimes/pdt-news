(function () {
  var KEY = "pdt-reader";
  var st = { mode: "white", font: "news", size: 18, cols: 0, width: "wide" };
  try { Object.assign(st, JSON.parse(localStorage.getItem(KEY) || "{}")); } catch (e) {}

  function apply() {
    document.documentElement.setAttribute("data-mode", st.mode);
    document.documentElement.setAttribute("data-font", st.font);
    document.documentElement.style.setProperty("--fs", st.size + "px");
    document.querySelectorAll(".prose").forEach(function (el) {
      el.classList.toggle("columns", st.cols > 0);
      if (st.cols > 0) {
        var screen = st.width === "narrow" ? 1280 : st.width === "4k" ? 3840 : st.width === "2k" ? 2560 : 1920;
        var colW = screen / st.cols;
        var n = Math.max(1, Math.floor(el.clientWidth / colW));
        el.style.columnCount = String(n);
      } else {
        el.style.columnCount = "";
      }
    });
    localStorage.setItem(KEY, JSON.stringify(st));
  }

  function dock() {
    var d = document.getElementById("readerDock");
    if (!d) return;
    d.hidden = false;
    d.innerHTML =
      "<p><b>Reader</b></p>" +
      row("Paper", "mode", ["white", "black", "dark", "gray", "beige"]) +
      row("Face", "font", [["news", "News (old-style)"], ["trans", "Transitional"], ["sans", "Sans"]]) +
      "<label>Size <input type=range min=14 max=28 value=" + st.size + " id=rSize></label>" +
      "<label>Columns / 16:9 screen <input type=range min=0 max=8 value=" + st.cols + " id=rCols> <span id=rColsN>" + (st.cols || "off") + "</span></label>" +
      row("Ref width", "width", [["wide", "1080p"], ["2k", "2K"], ["4k", "4K"], ["narrow", "narrow"]]) +
      "<p class=hint>0 columns = off. 8 at half a 1080p window becomes 4.</p>" +
      "<p><button type=button id=rCopy>Copy link</button></p>";
    d.querySelectorAll("[data-k]").forEach(function (b) {
      b.onclick = function () { st[b.dataset.k] = b.dataset.v; apply(); dock(); };
    });
    d.querySelector("#rSize").oninput = function () { st.size = +this.value; apply(); };
    d.querySelector("#rCols").oninput = function () {
      st.cols = +this.value; d.querySelector("#rColsN").textContent = st.cols || "off"; apply();
    };
    d.querySelector("#rCopy").onclick = function () {
      var u = document.querySelector(".thread") && document.querySelector(".thread").dataset.url || location.href;
      navigator.clipboard.writeText(u);
      this.textContent = "Copied";
    };
  }
  function row(label, k, opts) {
    var html = "<p>" + label + " ";
    opts.forEach(function (o) {
      var v = Array.isArray(o) ? o[0] : o;
      var t = Array.isArray(o) ? o[1] : o;
      html += "<button type=button data-k=" + k + " data-v=" + v + ">" + t + "</button> ";
    });
    return html + "</p>";
  }

  document.addEventListener("DOMContentLoaded", function () {
    apply();
    var btn = document.getElementById("readerBtn");
    if (btn) btn.onclick = function () {
      var d = document.getElementById("readerDock");
      if (d.hidden) dock(); else d.hidden = true;
    };
    window.addEventListener("resize", apply);
    document.querySelectorAll("[data-badad]").forEach(function (el) {
      var src = "/static/js/badad-embed.js";
      var s = document.createElement("script");
      s.src = src;
      s.async = true;
      document.body.appendChild(s);
    });
  });
})();
