(() => {
  const sheet = document.getElementById("sheet");
  const wrap = document.getElementById("sheet-wrap");
  const tpl = document.getElementById("card-tpl");
  const warn = document.getElementById("print-warn");
  const printBtn = document.getElementById("print-btn");
  const segButtons = Array.from(document.querySelectorAll(".seg button"));

  let party = null;
  let per = 6;
  const qrSrc = new URL("api/qr?size=900&t=" + Date.now(), window.location.href).href;

  function phoneToken() {
    try {
      return localStorage.getItem("party-phone-token") || "";
    } catch (_) {
      return "";
    }
  }

  function showWarn(msg) {
    warn.hidden = !msg;
    warn.textContent = msg || "";
  }

  function render() {
    sheet.className = "per-" + per;
    sheet.innerHTML = "";
    if (!party) return;
    for (let i = 0; i < per; i++) {
      const card = tpl.content.firstElementChild.cloneNode(true);
      card.querySelector(".qr-card-party").textContent = party.name || "Our party";
      card.querySelector(".qr-card-code").src = qrSrc;
      sheet.appendChild(card);
    }
    fit();
  }

  // Shrink the paper preview to fit narrow screens; printing ignores this.
  function fit() {
    sheet.style.transform = "";
    wrap.style.height = "";
    const cs = getComputedStyle(wrap);
    const avail = Math.min(wrap.clientWidth, document.documentElement.clientWidth) -
      parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
    const full = sheet.offsetWidth;
    if (full > avail && avail > 0) {
      const scale = avail / full;
      sheet.style.transform = "scale(" + scale + ")";
      wrap.style.height = sheet.offsetHeight * scale + "px";
    }
  }

  segButtons.forEach((btn) => {
    btn.addEventListener("click", () => {
      per = Number(btn.dataset.per) || 6;
      segButtons.forEach((b) => b.setAttribute("aria-pressed", String(b === btn)));
      render();
    });
  });

  printBtn.addEventListener("click", () => {
    // Wait for every QR image so none print blank.
    const imgs = Array.from(sheet.querySelectorAll("img"));
    Promise.all(imgs.map((img) => (img.complete ? null : img.decode().catch(() => null)))).then(() => window.print());
  });

  window.addEventListener("resize", fit);
  window.addEventListener("beforeprint", () => {
    sheet.style.transform = "";
    wrap.style.height = "";
  });
  window.addEventListener("afterprint", fit);

  async function load() {
    const headers = {};
    const token = phoneToken();
    if (token) headers.Authorization = "Bearer " + token;
    let data = {};
    try {
      const res = await fetch(new URL("api/status", window.location.href), { credentials: "same-origin", headers });
      data = await res.json();
    } catch (_) {
      showWarn("Could not reach Party Cam. Check your connection and reload.");
      return;
    }
    if (!data.party) {
      showWarn("Open your party first, then come back to print its cards.");
      return;
    }
    party = data.party;
    document.title = "Print QR cards · " + (party.name || "Party Cam");
    const cloud = party.cloud || {};
    if (!cloud.ready) {
      showWarn("Heads up: this party isn't saving to a cloud folder yet, so photos would stay on Party Cam. Connect Google Drive, Dropbox, or OneDrive on the party page before printing, so the cards are true.");
    }
    printBtn.disabled = false;
    render();
  }

  load();
})();
