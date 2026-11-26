// Shows a fixed "under development" banner whenever the server is in debug mode
// (PARTY_ENV is not "production"). Loaded by every page.
(() => {
  fetch(new URL("api/status", document.baseURI), { cache: "no-store", credentials: "same-origin" })
    .then((res) => res.json())
    .then((data) => {
      if (!data || data.debug !== true) return;
      const bar = document.createElement("div");
      bar.id = "debug-banner";
      bar.setAttribute("role", "status");
      bar.textContent = "This site is under development. Extra data may be collected to help catch bugs.";
      document.body.appendChild(bar);
      const size = () => document.documentElement.style.setProperty("--banner-h", bar.offsetHeight + "px");
      size();
      window.addEventListener("resize", size);
    })
    .catch(() => {});
})();
