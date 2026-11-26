(() => {
  const loginEl = document.getElementById("login");
  const homeEl = document.getElementById("home");
  const host = document.getElementById("host");
  const startForm = document.getElementById("start-form");
  const partyName = document.getElementById("party-name");
  const partyTitle = document.getElementById("party-title");
  const cloudNote = document.getElementById("cloud-note");
  const sourceButtons = document.getElementById("source-buttons");
  const noSources = document.getElementById("no-sources");
  const disconnectBtn = document.getElementById("disconnect-btn");
  const changeFolderBtn = document.getElementById("change-folder-btn");
  const cloudStatus = document.getElementById("cloud-status");
  const foldersEl = document.getElementById("folders");
  const folderList = document.getElementById("folder-list");
  const folderEmpty = document.getElementById("folder-empty");
  const folderCrumb = document.getElementById("folder-crumb");
  const newFolderForm = document.getElementById("new-folder-form");
  const newFolderName = document.getElementById("new-folder-name");
  const qr = document.getElementById("qr");
  const joinUrl = document.getElementById("join-url");
  const shotCount = document.getElementById("shot-count");
  const wallEmpty = document.getElementById("wall-empty");
  const wallList = document.getElementById("wall-list");
  const toastEl = document.getElementById("toast");
  const oauthError = document.getElementById("oauth-error");
  const textForm = document.getElementById("text-form");
  const loginPhoneForm = document.getElementById("login-phone-form");
  const loginCodeForm = document.getElementById("login-code-form");
  const loginPhone = document.getElementById("login-phone");
  const loginCode = document.getElementById("login-code");
  const partyList = document.getElementById("party-list");
  const partyEmpty = document.getElementById("party-empty");
  const homePhone = document.getElementById("home-phone");
  const logoutBtn = document.getElementById("phone-logout");
  const backParties = document.getElementById("back-parties");
  const showQr = document.getElementById("show-qr");
  const showUrl = document.getElementById("show-url");
  const showOpen = document.getElementById("show-open");
  const showCopy = document.getElementById("show-copy");
  const showReset = document.getElementById("show-reset");

  const tokenKey = "party-phone-token";
  let folderParent = "";
  let pollTimer = null;
  let connectedSource = "google";
  let loginPhoneValue = "";
  let loggedIn = false;

  function appBase() {
    const u = new URL(window.location.href);
    let path = u.pathname || "/";
    if (!path.endsWith("/")) {
      const last = path.split("/").pop() || "";
      path = /\.[a-z0-9]+$/i.test(last) ? path.slice(0, path.length - last.length) : path + "/";
    }
    u.pathname = path;
    u.search = "";
    u.hash = "";
    return u;
  }

  function appURL(path) {
    return new URL(path, appBase()).href;
  }

  function phoneToken() {
    try {
      return localStorage.getItem(tokenKey) || "";
    } catch (_) {
      return "";
    }
  }

  function setPhoneToken(token) {
    try {
      if (token) localStorage.setItem(tokenKey, token);
      else localStorage.removeItem(tokenKey);
    } catch (_) {}
    syncLogout();
  }

  function syncLogout() {
    logoutBtn.hidden = !loggedIn;
    document.body.classList.toggle("phone-in", loggedIn);
  }

  async function api(url, opts) {
    const next = Object.assign({ credentials: "same-origin" }, opts || {});
    const headers = Object.assign({}, next.headers || {});
    const token = phoneToken();
    if (token) headers.Authorization = "Bearer " + token;
    next.headers = headers;
    return fetch(appURL(url), next);
  }

  async function getJSON(url) {
    const res = await api(url);
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || res.statusText || "Request failed");
    return data;
  }

  async function postJSON(url, body) {
    const res = await api(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    });
    const text = await res.text();
    let data = {};
    try { data = JSON.parse(text); } catch (_) {}
    if (!res.ok) throw new Error(text || "Request failed");
    return data;
  }

  function toast(msg) {
    toastEl.hidden = !msg;
    toastEl.textContent = msg || "";
    if (msg) {
      setTimeout(() => {
        if (toastEl.textContent === msg) toastEl.hidden = true;
      }, 2800);
    }
  }

  function showOAuthError() {
    const params = new URLSearchParams(window.location.search);
    const msg = params.get("oauth_error");
    if (!msg) {
      oauthError.hidden = true;
      return false;
    }
    oauthError.hidden = false;
    oauthError.textContent = msg;
    params.delete("oauth_error");
    const next = params.toString();
    history.replaceState({}, "", window.location.pathname + (next ? "?" + next : ""));
    return true;
  }

  function sourceLabel(id) {
    if (id === "google") return "Google Drive";
    if (id === "dropbox") return "Dropbox";
    if (id === "onedrive") return "OneDrive";
    return id;
  }

  function renderSources(data) {
    const sources = (data && data.sources) || {};
    sourceButtons.innerHTML = "";
    const available = Object.keys(sources).filter((k) => sources[k]);
    noSources.hidden = available.length > 0;
    available.forEach((id) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.textContent = "Connect " + sourceLabel(id);
      btn.addEventListener("click", () => {
        window.location.href = appURL("api/oauth/" + id + "/start");
      });
      sourceButtons.appendChild(btn);
    });
  }

  function stopPoll() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  function showLogin() {
    stopPoll();
    loggedIn = false;
    loginEl.hidden = false;
    homeEl.hidden = true;
    host.hidden = true;
    loginPhoneForm.hidden = false;
    loginCodeForm.hidden = true;
    loginCode.value = "";
    syncLogout();
  }

  function renderPartyList(parties) {
    const list = parties || [];
    partyList.innerHTML = "";
    partyEmpty.hidden = list.length > 0;
    list.forEach((item) => {
      const li = document.createElement("li");
      const meta = document.createElement("div");
      meta.className = "meta";
      const title = document.createElement("strong");
      title.textContent = item.name || "Party";
      const detail = document.createElement("span");
      const n = item.photos || 0;
      const when = item.createdAt ? new Date(item.createdAt).toLocaleString() : "";
      detail.textContent = (n === 1 ? "1 photo" : n + " photos") + (when ? " · " + when : "");
      meta.appendChild(title);
      meta.appendChild(detail);
      const open = document.createElement("button");
      open.type = "button";
      open.textContent = "Open";
      open.addEventListener("click", () => {
        postJSON("api/parties/open", { id: item.id })
          .then(showHost)
          .catch((err) => toast(err.message || "Could not open that party"));
      });
      li.appendChild(meta);
      li.appendChild(open);
      partyList.appendChild(li);
    });
  }

  function showHome(data) {
    stopPoll();
    loggedIn = true;
    if (data && data.token) setPhoneToken(data.token);
    loginEl.hidden = true;
    homeEl.hidden = false;
    host.hidden = true;
    homePhone.textContent = data && data.phone ? "Parties on " + data.phone : "Your parties";
    renderPartyList(data && data.parties);
    renderSources(data);
    syncLogout();
  }

  async function loadFolders(parent) {
    folderParent = parent || "";
    folderCrumb.textContent = folderParent ? "Inside this folder" : "Top of the drive";
    const res = await getJSON("api/folders" + (folderParent ? "?parent=" + encodeURIComponent(folderParent) : ""));
    const folders = res.folders || [];
    folderList.innerHTML = "";
    folderEmpty.hidden = folders.length > 0;
    folders.forEach((folder) => {
      const li = document.createElement("li");
      const open = document.createElement("button");
      open.type = "button";
      open.textContent = folder.name;
      open.addEventListener("click", () => loadFolders(folder.id || folder.path).catch((err) => toast(err.message)));
      const use = document.createElement("button");
      use.type = "button";
      use.textContent = "Use this folder";
      use.addEventListener("click", () => selectFolder(folder).catch((err) => toast(err.message)));
      li.appendChild(open);
      li.appendChild(use);
      folderList.appendChild(li);
    });
  }

  async function selectFolder(folder) {
    const data = await postJSON("api/folder", {
      id: folder.id || "",
      name: folder.name || "",
      path: folder.path || "",
    });
    await showHost(data);
    toast("Shots will land in " + (folder.name || "that folder") + ".");
  }

  async function showHost(data) {
    loggedIn = true;
    if (data && data.token) setPhoneToken(data.token);
    loginEl.hidden = true;
    homeEl.hidden = true;
    host.hidden = false;
    const party = data.party || {};
    const cloud = party.cloud || {};
    partyTitle.textContent = party.name || "Your party";
    renderSources(data);
    if (cloud.source) connectedSource = cloud.source;
    disconnectBtn.hidden = !cloud.connected;
    sourceButtons.hidden = !!cloud.connected;
    changeFolderBtn.hidden = !cloud.ready;
    cloudStatus.hidden = !cloud.connected;
    const label = cloud.label || "the cloud";
    const account = cloud.account ? " (" + cloud.account + ")" : "";
    if (!cloud.connected) {
      cloudNote.textContent = "Connect a cloud so every guest photo streams into one folder.";
      cloudStatus.textContent = "";
      foldersEl.hidden = true;
    } else if (!cloud.ready) {
      cloudNote.textContent = "Connected to " + label + account + ". Choose the folder.";
      cloudStatus.textContent = "Connected to " + label + account + ". No folder chosen yet.";
      foldersEl.hidden = false;
      await loadFolders("");
    } else {
      const folderName = cloud.folderName || cloud.folderPath || "the party folder";
      cloudNote.textContent = "Saving to " + label + " / " + folderName + ".";
      cloudStatus.innerHTML = "";
      cloudStatus.append("Photos are going to ");
      const strong = document.createElement("strong");
      strong.textContent = label + " / " + folderName;
      cloudStatus.append(strong, account + ".");
      foldersEl.hidden = true;
    }
    joinUrl.textContent = party.joinUrl || "";
    qr.hidden = false;
    qr.src = appURL("api/qr?") + Date.now();
    showUrl.textContent = party.showUrl || "";
    showOpen.href = party.showUrl || "#";
    showQr.hidden = !party.showUrl;
    showQr.src = appURL("api/show-qr?") + Date.now();
    const n = party.photoCount || 0;
    shotCount.textContent = n === 0 ? "No photos yet." : n === 1 ? "1 photo in the album." : n + " photos in the album.";
    await loadWall();
    if (!pollTimer) pollTimer = setInterval(() => loadWall().catch(() => {}), 4000);
    syncLogout();
  }

  async function loadWall() {
    const data = await getJSON("api/shots");
    const shots = data.shots || [];
    wallEmpty.hidden = shots.length > 0;
    wallList.innerHTML = "";
    shots.forEach((shot) => {
      const li = document.createElement("li");
      const img = document.createElement("img");
      img.alt = shot.name || "Party photo";
      img.src = appURL("api/shot/" + shot.id);
      li.appendChild(img);
      wallList.appendChild(li);
    });
    const n = data.count || shots.length;
    shotCount.textContent = n === 0 ? "No photos yet." : n === 1 ? "1 photo in the album." : n + " photos in the album.";
  }

  loginPhoneForm.addEventListener("submit", (e) => {
    e.preventDefault();
    loginPhoneValue = loginPhone.value.trim();
    postJSON("api/phone/send", { phone: loginPhoneValue })
      .then((data) => {
        loginCodeForm.hidden = false;
        if (data && data.debugCode) {
          loginCode.value = data.debugCode;
          toast("Debug: code is " + data.debugCode);
        } else {
          toast("Code sent. Check your texts.");
        }
        loginCode.focus();
      })
      .catch((err) => toast(err.message || "Could not send a code"));
  });

  loginCodeForm.addEventListener("submit", (e) => {
    e.preventDefault();
    postJSON("api/phone/verify", { phone: loginPhoneValue, code: loginCode.value.trim() })
      .then((data) => {
        if (data.token) setPhoneToken(data.token);
        showHome(data);
      })
      .catch((err) => toast(err.message || "Could not verify"));
  });

  startForm.addEventListener("submit", (e) => {
    e.preventDefault();
    postJSON("api/party", { name: partyName.value.trim() })
      .then(showHost)
      .catch((err) => toast(err.message || "Could not start a party"));
  });

  backParties.addEventListener("click", () => {
    getJSON("api/parties")
      .then(showHome)
      .catch((err) => toast(err.message || "Could not load parties"));
  });

  logoutBtn.addEventListener("click", () => {
    postJSON("api/phone/logout", {})
      .catch(() => {})
      .then(() => {
        setPhoneToken("");
        showLogin();
        toast("Logged out.");
      });
  });

  showCopy.addEventListener("click", () => {
    const link = showUrl.textContent;
    if (!link) return;
    navigator.clipboard.writeText(link)
      .then(() => toast("Slideshow link copied."))
      .catch(() => toast("Copy failed. Press and hold the link to copy it."));
  });

  showReset.addEventListener("click", () => {
    if (!window.confirm("Make a new slideshow link? Screens using the old link will stop updating.")) return;
    postJSON("api/show/reset", {})
      .then((data) => showHost(data))
      .then(() => toast("New slideshow link ready."))
      .catch((err) => toast(err.message || "Could not make a new link"));
  });

  changeFolderBtn.addEventListener("click", () => {
    foldersEl.hidden = false;
    loadFolders("").catch((err) => toast(err.message));
    foldersEl.scrollIntoView({ behavior: "smooth", block: "start" });
  });

  disconnectBtn.addEventListener("click", () => {
    postJSON("api/oauth/" + connectedSource + "/disconnect", {})
      .then(showHost)
      .catch((err) => toast(err.message));
  });

  newFolderForm.addEventListener("submit", (e) => {
    e.preventDefault();
    postJSON("api/folders", { name: newFolderName.value.trim() || partyTitle.textContent, parent: folderParent })
      .then((data) => selectFolder(data.folder || {}))
      .catch((err) => toast(err.message || "Could not create that folder"));
  });

  textForm.addEventListener("submit", (e) => {
    e.preventDefault();
    const phone = document.getElementById("text-phone").value.trim();
    if (!phone) return toast("Enter a phone number");
    postJSON("api/text-link", { phone })
      .then(() => toast("Sent the guest link."))
      .catch((err) => toast(err.message || "Could not send that text"));
  });

  const oauthReturned = showOAuthError() || new URLSearchParams(window.location.search).has("connected");
  if (new URLSearchParams(window.location.search).has("connected")) {
    const params = new URLSearchParams(window.location.search);
    params.delete("connected");
    const next = params.toString();
    history.replaceState({}, "", window.location.pathname + (next ? "?" + next : ""));
  }
  syncLogout();
  getJSON("api/status")
    .then((data) => {
      if (data.needsLogin) {
        setPhoneToken("");
        showLogin();
        return;
      }
      if (oauthReturned && data.party) return showHost(data);
      showHome(data);
    })
    .catch(() => {
      showLogin();
      toast("Could not load Party Cam.");
    });
})();
