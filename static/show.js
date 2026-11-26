(() => {
  const SLIDE_MS = 7000;
  const NEW_DELAY_MS = 1200;
  const POLL_MS = 4000;

  const slides = [document.getElementById("slide-a"), document.getElementById("slide-b")];
  const emptyEl = document.getElementById("show-empty");
  const emptyName = document.getElementById("show-empty-name");
  const emptyMsg = document.getElementById("show-empty-msg");
  const nameEl = document.getElementById("show-name");
  const countEl = document.getElementById("show-count");
  const newEl = document.getElementById("show-new");
  const joinEl = document.getElementById("show-join");
  const joinQr = document.getElementById("show-join-qr");
  const fsBtn = document.getElementById("show-fs");

  const key = (window.location.pathname.split("/").filter(Boolean).pop() || "").trim();
  const order = []; // every shot id, oldest first
  const known = new Set();
  const fresh = []; // ids that arrived since the page loaded, shown next
  let firstLoad = true;
  let front = 0;
  let cursor = -1;
  let timer = null;
  let showing = "";
  let dead = false;

  function appBase() {
    const u = new URL(window.location.href);
    u.pathname = u.pathname.replace(/\/show\/[^/]+\/?$/, "/");
    u.search = "";
    u.hash = "";
    return u;
  }

  function appURL(path) {
    return new URL(path, appBase()).href;
  }

  function shotURL(id) {
    return appURL("api/show/" + encodeURIComponent(key) + "/shot/" + encodeURIComponent(id));
  }

  function setCount(n) {
    countEl.textContent = n === 1 ? "1 photo" : n + " photos";
  }

  async function poll() {
    if (dead) return;
    let data;
    try {
      const res = await fetch(appURL("api/show/" + encodeURIComponent(key)), { cache: "no-store" });
      if (res.status === 404) {
        dead = true;
        stop();
        slides.forEach((s) => s.classList.remove("on"));
        emptyEl.hidden = false;
        emptyMsg.textContent = "This slideshow link was turned off. Ask the host for the new one.";
        return;
      }
      if (!res.ok) return;
      data = await res.json();
    } catch (_) {
      return; // Wi-Fi blip: keep showing what we have.
    }
    nameEl.textContent = data.name || "Party Cam";
    emptyName.textContent = data.name || "Party Cam";
    document.title = (data.name || "Party Cam") + " · slideshow";
    setCount(data.count || (data.shots || []).length);

    let added = false;
    (data.shots || []).forEach((shot) => {
      if (known.has(shot.id)) return;
      known.add(shot.id);
      order.push(shot.id);
      if (!firstLoad) {
        fresh.push(shot.id);
        added = true;
      }
    });

    if (firstLoad) {
      firstLoad = false;
      if (order.length) {
        // Start on the newest photo, then loop through the whole album.
        cursor = order.length - 2;
        advance();
      }
    } else if (added) {
      // Cut over to new photos quickly instead of waiting out the current slide.
      schedule(showing ? NEW_DELAY_MS : 0);
    }
    emptyEl.hidden = order.length > 0;
  }

  function nextId() {
    if (fresh.length) return { id: fresh.shift(), isNew: true };
    if (!order.length) return null;
    cursor = (cursor + 1) % order.length;
    return { id: order[cursor], isNew: false };
  }

  function schedule(ms) {
    clearTimeout(timer);
    timer = setTimeout(advance, ms);
  }

  function stop() {
    clearTimeout(timer);
    timer = null;
  }

  async function advance() {
    const next = nextId();
    if (!next) return;
    if (next.id === showing && order.length > 1 && !next.isNew) {
      return advance();
    }
    const url = shotURL(next.id);
    const incoming = slides[1 - front];
    const img = incoming.querySelector("img");
    const bg = incoming.querySelector(".slide-bg");
    img.src = url;
    try {
      await img.decode();
    } catch (_) {
      // Photo missing on the server: drop it and move on.
      const i = order.indexOf(next.id);
      if (i >= 0) {
        order.splice(i, 1);
        if (cursor >= i) cursor--;
      }
      schedule(300);
      return;
    }
    bg.style.backgroundImage = 'url("' + url + '")';
    incoming.classList.remove("on");
    void incoming.offsetWidth; // restart the slow zoom
    incoming.classList.add("on");
    slides[front].classList.remove("on");
    front = 1 - front;
    showing = next.id;
    newEl.hidden = !next.isNew;
    if (next.isNew) {
      // Let a new arrival's place in the loop follow on from here.
      cursor = order.indexOf(next.id);
    }
    schedule(fresh.length ? Math.max(SLIDE_MS / 2, 3000) : SLIDE_MS);
  }

  // Keep the tablet awake while the slideshow is up.
  let wakeLock = null;
  async function keepAwake() {
    try {
      if ("wakeLock" in navigator && document.visibilityState === "visible") {
        wakeLock = await navigator.wakeLock.request("screen");
      }
    } catch (_) {
      wakeLock = null;
    }
  }
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") {
      keepAwake();
      poll();
    }
  });

  // Full screen, and tuck the button away when nobody is touching the screen.
  const canFullscreen = !!(document.documentElement.requestFullscreen || document.documentElement.webkitRequestFullscreen);
  fsBtn.hidden = !canFullscreen;
  fsBtn.addEventListener("click", () => {
    const el = document.documentElement;
    if (document.fullscreenElement || document.webkitFullscreenElement) {
      (document.exitFullscreen || document.webkitExitFullscreen).call(document);
    } else {
      (el.requestFullscreen || el.webkitRequestFullscreen).call(el);
    }
    keepAwake();
  });
  document.addEventListener("fullscreenchange", () => {
    fsBtn.textContent = document.fullscreenElement ? "Exit full screen" : "Full screen";
  });
  let idleTimer = null;
  function wake() {
    document.body.classList.remove("idle");
    clearTimeout(idleTimer);
    idleTimer = setTimeout(() => document.body.classList.add("idle"), 3000);
  }
  ["mousemove", "touchstart", "keydown"].forEach((ev) => document.addEventListener(ev, wake, { passive: true }));
  document.addEventListener("keydown", (e) => {
    if (e.key === "ArrowRight" || e.key === " ") {
      e.preventDefault();
      advance();
    }
  });
  wake();

  joinQr.src = appURL("api/show/" + encodeURIComponent(key) + "/qr");
  joinQr.addEventListener("load", () => { joinEl.hidden = false; });

  keepAwake();
  poll();
  setInterval(poll, POLL_MS);
})();
