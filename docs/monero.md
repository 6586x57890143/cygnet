# Monero support

The Worker keeps serving the checkout. A private view-only wallet service scans
through a remote node; cygnet alone reads its RPC. Neither cygnet nor Merlin receives
the seed, spend key or view key. Merlin keeps using `cygnet.supporters` for roles and
the public `cygnet.payment_methods` for its advert. No wallet secret reaches the edge.

## Recovery material and runtime secrets

Create a dedicated wallet on a trusted personal device using a verified Monero
wallet. Cake Wallet on Android can hold the spending wallet; a legacy 25-word seed
provides straightforward CLI recovery. Save the seed, primary address, restore height and wallet password in a
Bitwarden secure note, with an independent recovery backup. The seed controls the
funds. Generating it on the server would put it inside that server's trust boundary.

Create a view-only copy using the primary address and private view key. This can
be done interactively in the isolated Linux CLI image when Windows antivirus blocks
the Windows binaries. Never supply the seed or spend key to that image. Give this
copy a separate wallet-file password. Only this copy goes to the server. Treat its
files and backups as private: the view key reveals incoming activity. The remote
node receives neither wallet key, but remains a trust and availability dependency.
Use a maintained node over authenticated TLS; do not disable certificate checking.

Keep two separate runtime passwords in Bitwarden: the view-only wallet-file password
and a generated high-entropy RPC password. The bot reads only the RPC password from
a mounted file. The wallet service reads the wallet password and RPC login from its
own files. Personal Bitwarden sessions or API credentials are not needed on foundry.
Do not put secret values in command arguments, `.env`, Git, terminal history or logs.

## Prepare the optional service

`docker-compose.xmr.yml` is an optional override. It publishes no port and puts the
wallet on its own network with cygnet, plus a separate network for node egress. It
does not join the fleet or database networks. Loading the override requires
`CYGNET_XMR_IMAGE`, an immutable `name@sha256:...` image reviewed for the host's CPU
architecture and containing verified wallet binaries plus the offline startup guard.
The supplied `docker/monero/Dockerfile` builds Monero 0.18.5.4 from official archives,
checks the pinned signing-key fingerprint and signed release checksums, and uses a
digest-pinned distroless runtime. It supports AMD64 and ARM64. Build from the repository root:

```sh
docker buildx build --platform linux/arm64 -f docker/monero/Dockerfile --target wallet-rpc -t cygnet-xmr-rpc:0.18.5.4 --load .
docker buildx build --platform linux/arm64 -f docker/monero/Dockerfile --target wallet-cli -t cygnet-xmr-cli:0.18.5.4 --load .
```

The CLI target is for interactive creation; it is not the long-running service.
Publish the reviewed RPC image to an owned registry and use its registry digest in
`CYGNET_XMR_IMAGE`. An image or binary upgrade needs the same review as the service itself.

On Windows with Docker Desktop, `pwsh -File scripts/monero-smoke.ps1` rebuilds the
images and exercises disposable offline stagenet fixtures. It checks rejection of
a full wallet, startup of a view-only wallet, real TLS/Digest authentication,
stored subaddress batches, incoming reads and refusal to quote without a daemon.
It never uses personal wallet material or publishes a port. Failures leave the
labelled test containers available for inspection; successful runs remove them.

Release 0.18.5.4 lacks the newer `get_wallet_info` RPC and SHA-256 Digest options.
The startup guard instead inspects `wallet_info` offline, refuses any spend-enabled,
wrong-network or wrong-address wallet, validates the fixed service configuration,
then replaces itself with wallet RPC. Cygnet checks the address and a completed
`refresh` plus positive wallet height. Legacy Digest is accepted only through
certificate-verified TLS with a password of at least 32 characters; use the
48-character random RPC password described above. No unverified TLS or plaintext
legacy authentication fallback is permitted.

Prepare these host paths outside version control, owned by UID/GID 65532:

- `xmr-wallet/`: directory mode 0700, containing only the view-only wallet and cache
- `secrets/`: directory mode 0700
- `secrets/xmr-rpc-password`: RPC password only, file mode 0400
- `secrets/xmr-wallet-password`: view-only wallet password only, file mode 0400
- `secrets/xmr-wallet.conf`: configuration below, file mode 0400
- `secrets/xmr-rpc-tls.key`: wallet RPC TLS private key, file mode 0400
- `secrets/xmr-rpc-ca.crt`: pinned RPC certificate, file mode 0444

The operator writes these files privately over SSH using a non-echoing prompt or
secure file transfer; do not paste their contents into a command. The passwords must
match the corresponding Bitwarden entries. Never copy the full wallet to these paths.

Create a dedicated self-signed server certificate with SAN `DNS:xmr-wallet`. Keep
the private key only in the wallet container; cygnet mounts only the certificate.
For example, on the deployment host, before setting the file ownership and modes:

```sh
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc -sha256 -days 365 \
  -keyout secrets/xmr-rpc-tls.key -out secrets/xmr-rpc-ca.crt \
  -subj /CN=xmr-wallet -addext subjectAltName=DNS:xmr-wallet
```

Renew before expiry and update the pinned certificate in both services together.
No TLS private key is needed by cygnet or the Worker.

```ini
wallet-file=/wallet/support-view
password-file=/run/secrets/xmr-wallet-password
rpc-bind-ip=0.0.0.0
rpc-bind-port=18088
confirm-external-bind=1
rpc-login=cygnet:REPLACE_PRIVATELY_WITH_RPC_PASSWORD
rpc-ssl=enabled
rpc-ssl-private-key=/run/secrets/xmr-rpc-tls.key
rpc-ssl-certificate=/run/secrets/xmr-rpc-ca.crt
daemon-address=https://REPLACE_WITH_YOUR_REMOTE_NODE
daemon-ssl=enabled
daemon-ssl-ca-certificates=/etc/ssl/certs/ca-certificates.crt
daemon-ssl-allow-chained=1
untrusted-daemon=1
log-file=/tmp/wallet-rpc.log
log-level=0
max-log-file-size=1048576
max-log-files=1
```

The non-loopback bind is container-only. Do not add a Compose `ports` entry or an
Oracle firewall rule for it. The view-only wallet supplies the security boundary;
do not replace it with a hot wallet. The startup guard refuses spend-enabled files
before opening an RPC listener. Keep using the guarded image and entrypoint.

Set non-secret configuration in cygnet's environment:

```dotenv
CYGNET_SUPPORT_MONERO=YOUR_PRIMARY_ADDRESS
CYGNET_XMR_RPC=https://xmr-wallet:18088/json_rpc
CYGNET_XMR_RPC_USER=cygnet
CYGNET_XMR_RPC_PASSWORD_FILE=/run/secrets/xmr-rpc-password
CYGNET_XMR_RPC_CA_FILE=/run/secrets/xmr-rpc-ca.crt
CYGNET_XMR_NETWORK=Mainnet
```

Use a separate wallet and deployment with `Stagenet` for validation. Mainnet and
stagenet addresses are checksum-validated and cannot be mixed by configuration.
Add `stagenet=1` to the wallet configuration for that deployment. Cake Wallet is
mainnet-only; create the disposable stagenet wallets in the Linux test environment.
After reviewing the image, secrets and network configuration, the operator creates
the ignored `.xmr-enabled` marker in the deployment directory. CI and manual deploys
then retain the override on every update and rollback. Without that marker the
normal deployment is unchanged. Set `CYGNET_XMR_IMAGE` to the reviewed image digest
in the deployment's `.env`. To disable, first clear the XMR environment settings,
then remove the marker. Keep a copy of the previous deployment configuration.

## Payment and recovery behavior

The startup guard validates wallet identity, network and view-only mode. The watcher
pins identity again and requires a successful refresh before advertising readiness.
It preallocates subaddresses, saves the wallet, then imports them into Postgres.
Invoice creation allocates one in the same transaction as the invoice. A database
trigger prevents deleting or reassigning its address, even after expiry. Back up
the wallet files and allocation table together; keep encrypted backups off-host.

An XMR quote is valid for 20 minutes, with the existing 10 minute settlement slack.
Only receipts timestamped within that window count automatically. Ten confirmations,
unlocked funds, no extra unlock time and no observed double-spend are required.
Partial eligible receipts add up; overpayment grants the purchased period once.
Late or wrong payments remain for manual review. The immutable payment record and
supporter period commit together. Re-reading receipts cannot add backing again.

Current invoices have their own scan budget; historical addresses are checked in a
separate rotating batch. Wallet readiness expires after two minutes without a
healthy check. Prices come from a recent Kraken XMR/USD trade with a timestamp,
sanity bounds and a short cache. A failed or stale feed refuses new quotes.

For spending, open or restore the full wallet on the trusted personal machine and
sync it through a node. These are separate wallet files; the view-only service can
keep running. Ensure the restored wallet scans every allocated subaddress index:
keep the highest `support_address.minor` with the backup and set the CLI restore
subaddress lookahead above it. A seed alone does not preserve invoice mappings.
Import signed key images if an accurate spent balance in the view-only wallet is
needed; Cygnet settles from incoming receipts, never from that displayed balance.

## Before enabling mainnet

Exercise receipt replay, partial and late payments, wallet restart and restore on
stagenet. Confirm a paid checkout appears through Merlin's actual fleet link, grants
its timed role and posts the configured thank-you card. Merlin polls every ten
minutes; its existing thank-you delivery remains best-effort. Check expiry removes
the tracked role and renewal extends it. No production change is authorized merely
by adding this file or merging application code.

References: [wallet RPC](https://docs.getmonero.org/rpc-library/wallet-rpc/),
[view-only wallets](https://www.getmonero.org/resources/user-guides/view_only.html),
[CLI restore and lookahead](https://docs.getmonero.org/interacting/monero-wallet-cli-reference/),
[RPC options](https://docs.getmonero.org/interacting/monero-wallet-rpc-reference/),
[Kraken recent trades](https://docs.kraken.com/api-reference/market-data/get-recent-trades).
