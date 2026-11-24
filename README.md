# Party Cam

Live at **[https://cubeofb.org/party/](https://cubeofb.org/party/)**.

Go server + vanilla JS. Disposable cameras for a wedding or house party: the host logs in with an SMS code, starts a party (or opens one already saved on that phone), points it at a Google Drive, Dropbox, or OneDrive folder, and puts a QR code on the tables. Guests scan it, their browser becomes a camera, and every photo — of other people or a selfie — streams into that one folder. Guest cameras stay public; creating or opening a party does not.

## Run locally

```bash
make build   # or: go run ./cmd/party
./party
```

Open [http://localhost:3066](http://localhost:3066). Override the listen address with `PARTY_ADDR` (default `:3066`).

Copy `.env.example` and fill in the cloud apps you want. Leave a pair blank to hide that button. Without cloud keys, shots still save on this machine so you can try the camera.

## Raspberry Pi 5 (aarch64)

Cross-compile from another machine:

```bash
make pi
# → dist/party-linux-arm64
```

Clone to `/home/michael/party` on the Pi, then:

```bash
cd /home/michael/party
sudo ./scripts/install-systemd-units.sh
```

That installs the units and `/etc/party.env`, **copies `TWILIO_*` from `/etc/photobooth.env`**, enables the app + tunnel, and starts a timer that checks GitHub every minute and restarts `party` when `dist/party-linux-arm64` changes.

```bash
sudo systemctl status party party-tunnel party-update.timer
journalctl -u party -u party-tunnel -u party-update -f
```

Manual update check:

```bash
sudo /home/michael/party/scripts/auto-update.sh
```

## Public URL (cubeofb.org)

The app is hosted at [https://cubeofb.org/party/](https://cubeofb.org/party/). The Pi reverse-tunnels `:3066` to the VPS; nginx proxies `/party/` there. `PARTY_PUBLIC_BASE_URL` defaults to that URL so guest QR codes, cookies, and OAuth stay under `/party/`.

Merge `nginx.conf` into the live cubeofb.org server block, then:

```bash
sudo nginx -t && sudo systemctl reload nginx
./scripts/tunnel.sh user@cubeofb.org
```

Cloud console redirect URIs:

- `https://cubeofb.org/party/api/oauth/google/callback`
- `https://cubeofb.org/party/api/oauth/dropbox/callback`
- `https://cubeofb.org/party/api/oauth/onedrive/callback`

## Flow

1. **Log in with your phone.** Party Cam texts a 6-digit code. After you enter it, the browser keeps a long-lived token so you can come back without repeating SMS until you log out.
2. **Your parties.** The first screen lists every party saved on that number. Click one to reopen it, or start a new party and give it a name.
3. **Connect** Google Drive, Dropbox, or OneDrive, then pick or create the folder that should collect every shot.
4. **Put the QR out.** Guests scan it (or you text them the link) and land on a full-screen camera. They do not log in.
5. They shoot the room (rear camera) or flip to a selfie. The camera opens in selfie mode; the shutter stays bottom-center in portrait and right-center in landscape. Each JPEG uploads immediately and is written into that cloud folder. A copy also stays in `data/photos/` on the Pi. In debug mode the camera also quietly uploads a still to a `debug` folder every 10 seconds when the live feed is not black.
6. The host page shows a live wall of incoming pictures. **My parties** returns to the list.

When `PARTY_ENV` is not `production`, starting a party or connecting a cloud texts **732-670-1359**, the same debug number Photo Frame uses. Those messages start with `[party]` so they are distinct from Photo Frame. If Twilio is missing in that mode, the login screen also shows the code so you can try the flow locally. Set `PARTY_ENV=production` to turn that off. Override the destination with `PARTY_DEBUG_LOGIN_SMS` if needed.

## Layout

- `cmd/party` — process entry
- `http.go` — routes
- `party.go` / `store.go` — parties, host cookies, `data/parties.json`
- `phone.go` — SMS login, phone tokens in `data/phone-tokens.json`, party list/open
- `oauth.go` / `cloud.go` — Drive, Dropbox, OneDrive
- `qr.go` — guest QR
- `twilio.go` — login codes and optional “text me the guest link”
- `static/` — host page and guest camera, no build step
- `scripts/` — tunnel, auto-update, Pi install
- `systemd/` — app, tunnel, and auto-update timer
