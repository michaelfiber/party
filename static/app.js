(() => {
  const welcome = document.getElementById("welcome");
  const host = document.getElementById("host");
  const startForm = document.getElementById("start-form");
  const partyName = document.getElementById("party-name");
  const partyTitle = document.getElementById("party-title");
  const cloudNote = document.getElementById("cloud-note");
  const sourceButtons = document.getElementById("source-buttons");
  const noSources = document.getElementById("no-sources");
  const disconnectBtn = document.getElementById("disconnect-btn");
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

  let folderParent = "";
  let pollTimer = null;
  let connectedSource = "google";

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

  async function api(url, opts) {
    const next = Object.assign({ credentials: "same-origin" }, opts || {});
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
      return;
    }
    oauthError.hidden = false;
    oauthError.textContent = msg;
    params.delete("oauth_error");
    const next = params.toString();
    history.replaceState({}, "", window.location.pathname + (next ? "?" + next : ""));
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
    welcome.hidden = true;
    host.hidden = false;
    const party = data.party || {};
    const cloud = party.cloud || {};
    partyTitle.textContent = party.name || "Your party";
    renderSources(data);
    if (cloud.source) connectedSource = cloud.source;
    disconnectBtn.hidden = !cloud.connected;
    if (!cloud.connected) {
      cloudNote.textContent = "Connect a cloud so every guest photo streams into one folder.";
      foldersEl.hidden = true;
    } else if (!cloud.ready) {
      cloudNote.textContent = "Connected to " + (cloud.label || "the cloud") + " as " + (cloud.account || "you") + ". Choose the folder.";
      foldersEl.hidden = false;
      await loadFolders("");
    } else {
      cloudNote.textContent = "Saving to " + (cloud.label || "the cloud") + " / " + (cloud.folderName || cloud.folderPath || "the party folder") + ".";
      foldersEl.hidden = true;
    }
    joinUrl.textContent = party.joinUrl || "";
    qr.hidden = false;
    qr.src = appURL("api/qr?") + Date.now();
    const n = party.photoCount || 0;
    shotCount.textContent = n === 0 ? "No photos yet." : n === 1 ? "1 photo in the album." : n + " photos in the album.";
    await loadWall();
    if (!pollTimer) pollTimer = setInterval(() => loadWall().catch(() => {}), 4000);
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

  startForm.addEventListener("submit", (e) => {
    e.preventDefault();
    postJSON("api/party", { name: partyName.value.trim() })
      .then(showHost)
      .catch((err) => toast(err.message || "Could not start a party"));
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

  showOAuthError();
  getJSON("api/status")
    .then((data) => {
      if (data.needsParty) {
        welcome.hidden = false;
        host.hidden = true;
        renderSources(data);
        return;
      }
      return showHost(data);
    })
    .catch(() => {
      welcome.hidden = false;
      toast("Could not load this party.");
    });
})();
