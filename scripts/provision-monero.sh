#!/usr/bin/env bash
# Run by the owner in a private SSH terminal. Never run through an agent
# terminal: the view key is entered directly into the offline wallet CLI.
set +x
set -euo pipefail
umask 077
[[ -t 0 && -t 1 ]] || { echo 'Run interactively from your private SSH terminal.' >&2; exit 1; }
[[ $(id -u) == 0 ]] || { echo 'Run with sudo.' >&2; exit 1; }

runtime=/var/lib/cygnet-monero-runtime
base=$(cat "$runtime/project-dir")
image=$(cat "$runtime/image-ref")
[[ $base == /home/deploy/cygnet && $(realpath "$base") == "$base" && -f $base/docker-compose.prod.yml ]] || { echo 'Unexpected deployment directory.' >&2; exit 1; }
[[ $image =~ ^(cygnet-xmr-rpc@)?sha256:[0-9a-f]{64}$ ]] || { echo 'Expected an immutable wallet image.' >&2; exit 1; }
[[ $(docker image inspect --format '{{.Architecture}}' "$image") == arm64 ]] || { echo 'Expected the reviewed ARM64 image.' >&2; exit 1; }
for path in "$base/secrets" "$base/xmr-wallet" "$runtime/activation.env"; do
  [[ ! -e $path && ! -L $path ]] || { echo 'Wallet setup already exists; refusing to overwrite it.' >&2; exit 1; }
done

# This request contains no wallet material and verifies only the public tip.
height=$(python3 - <<'PY'
import json, urllib.request
with urllib.request.urlopen('https://xmr-node.cakewallet.com:18081/get_info', timeout=15) as response:
    info = json.load(response)
if info.get('status') != 'OK' or not info.get('mainnet') or not info.get('synchronized'):
    raise SystemExit('The TLS node is not ready on mainnet.')
print(int(info['height']))
PY
)
printf 'Node is synchronized at height %s.\n' "$height"
read -r -p 'Saved wallet restore height: ' restore_height
[[ $restore_height =~ ^[1-9][0-9]{0,8}$ ]] && (( restore_height <= height )) || { echo 'Invalid restore height.' >&2; exit 1; }
read -r -s -p 'View-only wallet-file password (48-128 ASCII letters/digits): ' wallet_password; printf '\n'
(( ${#wallet_password} >= 48 && ${#wallet_password} <= 128 )) || { echo 'The wallet-file password must be 48 to 128 characters long.' >&2; exit 1; }
[[ $wallet_password =~ ^[a-zA-Z0-9]+$ ]] || { echo 'The wallet-file password must contain only A-Z, a-z and 0-9: no symbols, spaces or pasted line endings.' >&2; exit 1; }
read -r -s -p 'RPC password (a different 48-128 ASCII letters/digits): ' rpc_password; printf '\n'
(( ${#rpc_password} >= 48 && ${#rpc_password} <= 128 )) || { echo 'The RPC password must be 48 to 128 characters long.' >&2; exit 1; }
[[ $rpc_password =~ ^[a-zA-Z0-9]+$ ]] || { echo 'The RPC password must contain only A-Z, a-z and 0-9: no symbols, spaces or pasted line endings.' >&2; exit 1; }
[[ $wallet_password != "$rpc_password" ]] || { echo 'The RPC password must differ from the wallet-file password.' >&2; exit 1; }

stage=$(mktemp -d "$base/.xmr-setup.XXXXXXXX")
cleanup() {
  unset wallet_password rpc_password info primary
  if [[ -n ${stage:-} && $stage == "$base"/.xmr-setup.* && -d $stage && ! -L $stage ]]; then
    rm -rf -- "$stage"
  fi
}
trap cleanup EXIT
install -d -m 0700 -o 65532 -g 65532 "$stage/wallet" "$stage/secrets"
printf '%s\n' "$wallet_password" > "$stage/secrets/xmr-wallet-password"
printf '%s\n' "$rpc_password" > "$stage/secrets/xmr-rpc-password"
chown 65532:65532 "$stage/secrets/xmr-wallet-password" "$stage/secrets/xmr-rpc-password"
chmod 0400 "$stage/secrets/xmr-wallet-password" "$stage/secrets/xmr-rpc-password"

echo 'The next program asks for your PRIMARY ADDRESS and PRIVATE VIEW KEY.'
echo 'Never enter your recovery phrase or spend key. This creates only a view-only wallet.'
echo 'After the wallet is created, type exit at its prompt to finish setup.'
docker run --rm -it --log-driver none --network none --read-only --user 65532:65532 \
  --cap-drop ALL --security-opt no-new-privileges:true --pids-limit 64 --memory 512m --cpus 1 \
  --tmpfs /tmp:size=16m,mode=1777 \
  --mount "type=bind,source=$stage/wallet,target=/wallet" \
  --mount "type=bind,source=$stage/secrets/xmr-wallet-password,target=/run/secrets/xmr-wallet-password,readonly" \
  --entrypoint /usr/local/bin/monero-wallet-cli "$image" \
  --offline --generate-from-view-key /wallet/support-view \
  --password-file /run/secrets/xmr-wallet-password --restore-height "$restore_height" --log-file /tmp/setup.log

info=$(docker run --rm --log-driver none --network none --read-only --user 65532:65532 \
  --cap-drop ALL --security-opt no-new-privileges:true --pids-limit 64 --memory 512m --cpus 1 \
  --tmpfs /tmp:size=16m,mode=1777 \
  --mount "type=bind,source=$stage/wallet,target=/wallet" \
  --mount "type=bind,source=$stage/secrets/xmr-wallet-password,target=/run/secrets/xmr-wallet-password,readonly" \
  --entrypoint /usr/local/bin/monero-wallet-cli "$image" \
  --offline --wallet-file /wallet/support-view --password-file /run/secrets/xmr-wallet-password \
  --log-file /tmp/check.log wallet_info)
[[ $(printf '%s\n' "$info" | grep -cx 'Type: Watch only') == 1 && $(printf '%s\n' "$info" | grep -cx 'Network type: Mainnet') == 1 ]] || { echo 'The new wallet failed its view-only/mainnet check.' >&2; exit 1; }
primary=$(printf '%s\n' "$info" | sed -nE 's/^Address: (4[1-9A-HJ-NP-Za-km-z]{94})$/\1/p')
[[ $primary =~ ^4[1-9A-HJ-NP-Za-km-z]{94}$ ]] || { echo 'The primary address could not be verified.' >&2; exit 1; }

openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc -sha256 -days 365 \
  -keyout "$stage/secrets/xmr-rpc-tls.key" -out "$stage/secrets/xmr-rpc-ca.crt" \
  -subj /CN=xmr-wallet -addext subjectAltName=DNS:xmr-wallet >/dev/null 2>&1
cat > "$stage/secrets/xmr-wallet.conf" <<EOF
wallet-file=/wallet/support-view
password-file=/run/secrets/xmr-wallet-password
rpc-bind-ip=0.0.0.0
rpc-bind-port=18088
confirm-external-bind=1
rpc-login=cygnet:$rpc_password
rpc-ssl=enabled
rpc-ssl-private-key=/run/secrets/xmr-rpc-tls.key
rpc-ssl-certificate=/run/secrets/xmr-rpc-ca.crt
daemon-address=https://xmr-node.cakewallet.com:18081
daemon-ssl=enabled
daemon-ssl-ca-certificates=/etc/ssl/certs/ca-certificates.crt
daemon-ssl-allow-chained=1
untrusted-daemon=1
log-file=/tmp/wallet-rpc.log
log-level=0
max-log-file-size=1048576
max-log-files=1
EOF
cat > "$stage/activation.env" <<EOF
CYGNET_XMR_IMAGE=$image
CYGNET_SUPPORT_MONERO=$primary
CYGNET_XMR_RPC=https://xmr-wallet:18088/json_rpc
CYGNET_XMR_RPC_USER=cygnet
CYGNET_XMR_RPC_PASSWORD_FILE=/run/secrets/xmr-rpc-password
CYGNET_XMR_RPC_CA_FILE=/run/secrets/xmr-rpc-ca.crt
CYGNET_XMR_NETWORK=Mainnet
EOF
chown -R 65532:65532 "$stage/secrets" "$stage/wallet"
chmod 0400 "$stage/secrets/"*
chmod 0444 "$stage/secrets/xmr-rpc-ca.crt"
chmod 0600 "$stage/activation.env"
# No existing files are replaced, and activation is a separate rollout step.
mv -T -- "$stage/wallet" "$base/xmr-wallet"
mv -T -- "$stage/secrets" "$base/secrets"
mv -T -- "$stage/activation.env" "$runtime/activation.env"
unset wallet_password rpc_password info primary
echo 'View-only wallet, private TLS and authentication files are prepared.'
echo 'No wallet listener was started and the active bot environment was not changed.'
