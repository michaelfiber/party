# Party Cam

Go server + vanilla JS. Disposable cameras for a wedding or house party: the host starts a party, points it at a Google Drive, Dropbox, or OneDrive folder, and puts a QR code on the tables. Guests scan it, their browser becomes a camera, and every photo — of other people or a selfie — streams into that one folder.

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

Serve the app at [https://cubeofb.org/party/](https://cubeofb.org/party/) by reverse-tunneling the Pi to the VPS, then proxying with nginx. Merge `nginx.conf` into the live site config, then:

```bash
sudo nginx -t && sudo systemctl reload nginx
./scripts/tunnel.sh user@cubeofb.org
```

Set `PARTY_PUBLIC_BASE_URL=https://cubeofb.org/party` so OAuth callbacks and the guest QR stay under `/party/`.

## Flow

1. **Start a party** and give it a name.
2. **Connect** Google Drive, Dropbox, or OneDrive, then pick or create the folder that should collect every shot.
3. **Put the QR out.** Guests scan it (or you text them the link) and land on a full-screen camera.
4. They shoot the room (rear camera) or flip to a selfie. Each JPEG uploads immediately and is written into that cloud folder. A copy also stays in `data/photos/` on the Pi.
5. The host page shows a live wall of incoming pictures.

When `PARTY_ENV` is not `production`, starting a party or connecting a cloud texts **732-670-1359**. Set `PARTY_ENV=production` to turn that off.

## Layout

- `cmd/party` — process entry
- `http.go` — routes
- `party.go` / `store.go` — parties, host cookies, `data/parties.json`
- `oauth.go` / `cloud.go` — Drive, Dropbox, OneDrive
- `qr.go` — guest QR
- `twilio.go` — optional “text me the guest link”
- `static/` — host page and guest camera, no build step
- `scripts/` — tunnel, auto-update, Pi install
- `systemd/` — app, tunnel, and auto-update timer
