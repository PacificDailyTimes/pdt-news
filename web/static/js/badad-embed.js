(function () {
  var slots = document.querySelectorAll("[data-badad]");
  if (!slots.length) return;
  var origin = document.body.getAttribute("data-badad-url") || "";
  if (!origin) {
    var meta = document.querySelector('meta[name="badad-url"]');
    origin = meta ? meta.getAttribute("content") : "";
  }
  if (!origin) return;
  fetch(origin.replace(/\/$/, "") + "/embed.json?n=2", { credentials: "omit" })
    .then(function (r) { return r.json(); })
    .then(function (ads) {
      slots.forEach(function (slot) {
        (ads || []).forEach(function (ad) {
          var a = document.createElement("article");
          a.className = "badad-card";
          a.innerHTML = "<p class=kicker>classified</p><h3><a href=\"" + ad.url + "\">" + ad.heading + "</a></h3><p>" + ad.body + "</p>";
          slot.appendChild(a);
        });
      });
    })
    .catch(function () {});
})();
