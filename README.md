# Channel Transition Hijacking PoC

This tool demonstrates a character-session hijacking issue in Cosmic's MapleStory v83 channel-transition flow. A second connection can enter the world as a character that is changing channels without authenticating as that character's account.

**For authorized security testing on a local development server only.** The tool restricts connections to loopback addresses. Use disposable accounts and do not run it against other players or a production server.

## How it works

1. Logs an observer character into the game through the normal login, PIN/PIC, character-selection, and channel-entry flow.
2. Learns the target character's public ID from `SPAWN_PLAYER` while both characters are on the same map.
3. Opens a second connection to the same channel and waits.
4. When the target's `REMOVE_PLAYER_FROM_MAP` packet arrives, sends `PLAYER_LOGGEDIN` for the target on the second connection.
5. Treats the resulting `SET_FIELD` packet as proof of entry and prints the target's stats, EXP, mesos, and map ID.

## Requirements

- Go 1.23 or newer.
- A local Cosmic server with login port `8484` and channel ports accessible from the host.
- `USE_IP_VALIDATION: false` in the server's `config.yaml` (the vulnerable configuration).
- An observer account and character for the Go tool.
- A separate target account and character that you control in the game client.
- Existing PIN/PIC credentials if required by the observer account. First-time PIN/PIC registration is not supported; complete registration with the game client first.

Start the server from the Cosmic repository, for example:

```bash
cd ~/Projects/ms/Cosmic
podman compose up -d
```

## Run the PoC

From this repository:

```bash
export POC_PASSWORD='observer-account-password'
export POC_PIN='observer-pin'       # Only if the account requires a PIN
export POC_PIC='observer-pic'       # Only if the account requires a PIC

go run . \
  -account OBSERVER_ACCOUNT \
  -character OBSERVER_CHARACTER \
  -target TARGET_CHARACTER
```

Example:

```bash
POC_PASSWORD='secret' POC_PIN='1234' POC_PIC='5678' go run . \
  -account testuser \
  -character Observer \
  -target Victim
```

The password, PIN, and PIC are read from environment variables rather than command-line arguments.

## Demonstration steps

1. Log the target character into the game client.
2. Place the target and observer characters on the **same map and channel**.
3. Start the Go tool using the observer account's credentials.
4. Wait for these messages:

   ```text
   Observer entered world: ...
   Target observed: id=... name=... level=...
   Second channel connection armed. Change channels with the game client now.
   ```

5. Change channels with the target character in the game client.
6. On success, the second connection receives the target's private character data:

   ```text
   SUCCESS: second connection entered the world as the target.
   id=... name=... level=... job=... hp=.../... mp=.../... exp=... mesos=... map=...
   ```

A failed race automatically closes and replaces the armed connection. Change channels again to retry without restarting the observer.

## Options

- `-host`: Server address; loopback only. Default: `127.0.0.1`.
- `-world`: Zero-based world ID. Default: `0`.
- `-channel`: One-based channel ID. Default: `1`.
- `-delay`: Delay between target removal and the transition attempt. Default: `75ms`.
- `-timeout`: Network operation timeout. Default: `10s`.
- `-attempt-timeout`: Maximum wait for each attempt. Default: `3s`.

If attempts consistently fail, adjust `-delay`. A very short delay can arrive before the target enters `LOGIN_SERVER_TRANSITION`; a long delay can let the legitimate game client complete the handoff first. For example:

```bash
go run . -account testuser -character Observer -target Victim -delay 125ms
```

## Troubleshooting

- **Character not found:** Check the observer character's spelling and selected world/channel.
- **Waiting for target:** Ensure both characters are on the same map and channel. Moving either character out of and back into the map can trigger a fresh spawn packet.
- **PIN/PIC registration unsupported:** Log in with the game client and complete registration before using the tool.
- **Attempt rejected or disconnected:** Change channels again and tune `-delay`.
- **Non-loopback address rejected:** This is intentional. Run the server locally or use an appropriate local development setup.

## Development checks

```bash
go test -race ./...
go vet ./...
go build ./...
```
