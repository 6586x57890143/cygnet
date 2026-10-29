#!/usr/bin/env python3
"""Drive the opt-in funded test using only a disposable public-stagenet wallet."""

import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import time

PASSWORD = "disposable-test-password"
RPC_PASSWORD = "disposable-test-password-for-tls-authentication"


def command(args, timeout=60, data=None):
    result = subprocess.run(args, input=data, capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError("Fixture command failed; raw output is retained only for the Go test log.")
    return result.stdout.strip()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("state", type=Path, help="state of the disposable stagenet faucet wallet")
    args = parser.parse_args()
    state = json.loads(args.state.read_text())
    root = Path(state["scratch"]).resolve()
    wallets = Path(state["wallets"]).resolve()
    if not root.is_relative_to(Path(tempfile.gettempdir()).resolve()) or not root.name.startswith("cygnet-xmr-funded-") or wallets != root / "wallets":
        raise RuntimeError("Unexpected disposable wallet directory.")
    if state["node"] != "https://xmr.nodely.dev:38089" or not re.fullmatch(r"5[1-9A-HJ-NP-Za-km-z]{94}", state["address"]):
        raise RuntimeError("Only the disposable stagenet fixture is accepted.")
    rpc_image, tools_image = state["rpc_image"], state["tools_image"]
    for image in [rpc_image, tools_image]:
        if not re.fullmatch(r"sha256:[0-9a-f]{64}", image):
            raise RuntimeError("Fixture images must be immutable.")
    setup = state["container"]
    metadata = json.loads(command(["docker", "inspect", setup]))[0]
    if metadata["Config"]["Labels"].get("codex.task") != "cygnet-xmr-funded" or metadata["State"]["Running"]:
        raise RuntimeError("Expected a stopped disposable initializer.")
    run = root / ("run-" + os.urandom(6).hex())
    run.mkdir()
    view, spend, secrets, checkpoints = [run / name for name in ["view", "spend", "secrets", "state"]]
    for directory in [view, spend, secrets, checkpoints]:
        directory.mkdir()
    suffix = os.urandom(6).hex()
    private, egress = "xmr-funded-private-" + suffix, "xmr-funded-egress-" + suffix
    view_name, spend_name, test_name = ["xmr-funded-" + kind + "-" + suffix for kind in ["view", "spend", "test"]]
    protections = ["--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--pids-limit", "64", "--cpus", "1", "--log-driver", "none", "--pull", "never"]
    created, networks, attached = [], [], False
    process = None

    def rpc(container, method, params, tls=False):
        options = ["docker", "run", "--rm", "-i", *protections, "--network", "container:" + container, "--memory", "128m"]
        if tls:
            options += ["--mount", f"type=bind,source={secrets}/xmr-rpc-ca.crt,target=/ca.crt,readonly"]
        options += ["--entrypoint", "curl", tools_image, "--silent", "--show-error", "--noproxy", "*", "--digest", "--user", "fixture:" + (RPC_PASSWORD if tls else PASSWORD), "--max-time", "30", "--header", "Content-Type: application/json"]
        if tls:
            options += ["--cacert", "/ca.crt"]
        options += ["--data-binary", "@-", ("https" if tls else "http") + "://127.0.0.1:18088/json_rpc"]
        reply = json.loads(command(options, data=json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params})))
        if reply.get("error") or "result" not in reply:
            raise RuntimeError("Fixture wallet refused " + method + "; no raw wallet output was displayed.")
        return reply["result"]

    def copy_wallet(source, destination):
        for extension in ["", ".keys"]:
            path = wallets / (source + extension)
            if path.is_symlink() or not path.is_file():
                raise RuntimeError("Unexpected fixture wallet file.")
            shutil.copyfile(path, destination / ("support-view" + extension))

    def wait_file(name, deadline):
        path = checkpoints / name
        while not path.exists():
            if process is not None and process.poll() is not None:
                raise RuntimeError("Funded test exited before its expected checkpoint.")
            if time.monotonic() > deadline:
                raise RuntimeError("Funded fixture checkpoint timed out.")
            time.sleep(2)
        return path

    try:
        command(["docker", "start", setup])
        time.sleep(2)
        rpc(setup, "open_wallet", {"filename": "funding-source", "password": PASSWORD})
        seed = rpc(setup, "query_key", {"key_type": "mnemonic"})["key"]
        rpc(setup, "close_wallet", {})
        restored = rpc(setup, "restore_deterministic_wallet", {"filename": "funded-spend-" + suffix, "password": PASSWORD, "seed": seed, "restore_height": state["restore_height"]})
        del seed
        if restored.get("address") != state["address"]:
            raise RuntimeError("Restored funding wallet has the wrong identity.")
        rpc(setup, "store", {})
        rpc(setup, "close_wallet", {})
        command(["docker", "stop", "--time", "20", setup])
        copy_wallet("funded-spend-" + suffix, spend)
        copy_wallet("funded-view", view)
        (secrets / "xmr-wallet-password").write_bytes((PASSWORD + "\n").encode("ascii"))
        command(["docker", "run", "--rm", "--network", "none", *protections, "--memory", "128m", "--tmpfs", "/tmp:size=16m,mode=1777", "--mount", f"type=bind,source={secrets},target=/out", "--entrypoint", "openssl", tools_image,
                 "req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-noenc", "-sha256", "-days", "2", "-keyout", "/out/xmr-rpc-tls.key", "-out", "/out/xmr-rpc-ca.crt", "-subj", "/CN=xmr-wallet", "-addext", "subjectAltName=DNS:xmr-wallet,IP:127.0.0.1"])
        # Docker Desktop maps host-file UIDs; this key is disposable test material.
        command(["docker", "run", "--rm", "--network", "none", *protections, "--memory", "64m", "--mount", f"type=bind,source={secrets},target=/out", "--entrypoint", "chmod", tools_image, "0444", "/out/xmr-rpc-tls.key"])
        configuration = {
            "wallet-file": "/wallet/support-view", "password-file": "/run/secrets/xmr-wallet-password", "rpc-bind-ip": "0.0.0.0", "rpc-bind-port": "18088", "confirm-external-bind": "1", "rpc-login": "fixture:" + RPC_PASSWORD,
            "rpc-ssl": "enabled", "rpc-ssl-private-key": "/run/secrets/xmr-rpc-tls.key", "rpc-ssl-certificate": "/run/secrets/xmr-rpc-ca.crt", "daemon-address": state["node"], "daemon-ssl": "enabled", "daemon-ssl-ca-certificates": "/etc/ssl/certs/ca-certificates.crt",
            "daemon-ssl-allow-chained": "1", "untrusted-daemon": "1", "log-file": "/tmp/wallet-rpc.log", "log-level": "0", "max-log-file-size": "1048576", "max-log-files": "1", "stagenet": "1",
        }
        (secrets / "xmr-wallet.conf").write_bytes("".join(key + "=" + value + "\n" for key, value in configuration.items()).encode("ascii"))
        for network, internal in [(private, True), (egress, False)]:
            command(["docker", "network", "create", "--label", "codex.task=cygnet-xmr-funded", *(["--internal"] if internal else []), network])
            networks.append(network)
        command(["docker", "network", "connect", private, "cygnet-payment-tests"])
        attached = True
        command(["docker", "create", "--name", view_name, "--label", "codex.task=cygnet-xmr-funded", *protections, "--network", private, "--network-alias", "xmr-wallet", "--memory", "512m", "--tmpfs", "/tmp:size=16m,mode=1777",
                 "--mount", f"type=bind,source={view},target=/wallet", "--mount", f"type=bind,source={secrets},target=/run/secrets,readonly", "--env", "CYGNET_XMR_PRIMARY=" + state["address"], "--env", "CYGNET_XMR_NETWORK=Stagenet", rpc_image])
        created.append(view_name)
        command(["docker", "network", "connect", egress, view_name])
        command(["docker", "start", view_name])
        spend_arguments = ["--stagenet", "--wallet-file", "/wallet/support-view", "--password-file", "/password", "--rpc-bind-ip", "127.0.0.1", "--rpc-bind-port", "18088", "--rpc-login", "fixture:" + PASSWORD, "--daemon-address", state["node"], "--daemon-ssl", "enabled", "--daemon-ssl-ca-certificates", "/etc/ssl/certs/ca-certificates.crt", "--daemon-ssl-allow-chained", "--untrusted-daemon", "--log-file", "/tmp/spend.log"]
        # Check the released parser before opening the funding wallet: bool
        # switches on this release accept neither a separate value nor '=1'.
        version = command(["docker", "run", "--rm", "--network", "none", *protections, "--memory", "128m", "--entrypoint", "/usr/local/bin/monero-wallet-rpc", rpc_image, *spend_arguments, "--version"])
        if "v0.18.5.4-release" not in version:
            raise RuntimeError("Unexpected released spending-fixture parser.")
        command(["docker", "run", "-d", "--name", spend_name, "--label", "codex.task=cygnet-xmr-funded", *protections, "--network", egress, "--memory", "512m", "--tmpfs", "/tmp:size=32m,mode=1777",
                 "--mount", f"type=bind,source={spend},target=/wallet", "--mount", f"type=bind,source={secrets}/xmr-wallet-password,target=/password,readonly", "--entrypoint", "/usr/local/bin/monero-wallet-rpc", rpc_image, *spend_arguments])
        created.append(spend_name)
        deadline = time.monotonic() + 45 * 60
        while True:
            try:
                balance = rpc(spend_name, "get_balance", {"account_index": 0})
                print(json.dumps({"funding_balance": balance["balance"], "funding_unlocked": balance["unlocked_balance"], "blocks_to_unlock": balance.get("blocks_to_unlock")}), flush=True)
                if balance["unlocked_balance"] >= 100_000_000_000:
                    break
            except (RuntimeError, ValueError):
                status = json.loads(command(["docker", "inspect", spend_name]))[0]["State"]
                if not status["Running"]:
                    raise RuntimeError("Disposable spending fixture exited; inspect its reviewed startup arguments before retrying.")
                print("Disposable funding wallet is synchronizing.", flush=True)
            if time.monotonic() > deadline:
                raise RuntimeError("Faucet funding did not unlock in time.")
            time.sleep(30)
        architecture = json.loads(command(["docker", "image", "inspect", rpc_image]))[0]["Architecture"]
        binary = run / "funded.test"
        environment = dict(os.environ, GOOS="linux", GOARCH=architecture, CGO_ENABLED="0")
        subprocess.run(["go", "test", "-c", "-trimpath", "-ldflags=-s -w", "-o", str(binary), "./internal/modules/support"], env=environment, check=True, timeout=180)
        arguments = ["docker", "run", "--rm", "--name", test_name, "--label", "codex.task=cygnet-xmr-funded", *protections, "--network", private, "--memory", "256m", "--tmpfs", "/tmp:size=4m,mode=1777", "--mount", f"type=bind,source={binary},target=/funded.test,readonly",
                     "--mount", f"type=bind,source={checkpoints},target=/fixture/state", "--mount", f"type=bind,source={secrets}/xmr-rpc-ca.crt,target=/fixture/xmr-rpc-ca.crt,readonly", "--env", "CYGNET_XMR_FUNDED_SMOKE=stagenet-faucet", "--env", "CYGNET_XMR_FUNDED_PRIMARY=" + state["address"],
                     "--env", "CYGNET_TEST_DATABASE_URL=postgres://cygnet:cygnet_test_local@cygnet-payment-tests:5432/cygnet?sslmode=disable", "--entrypoint", "/funded.test", rpc_image, "-test.run=^TestMoneroFundedStagenetSettlementAndRestore$", "-test.v", "-test.timeout=80m"]
        log = (run / "funded-check.log").open("w")
        process = subprocess.Popen(arguments, stdout=log, stderr=subprocess.STDOUT, text=True)
        created.append(test_name)
        invoices = json.loads(wait_file("invoices.json", time.monotonic() + 120).read_text())
        destinations = []
        for invoice in invoices:
            if not re.fullmatch(r"7[1-9A-HJ-NP-Za-km-z]{94}", invoice["Address"]):
                raise RuntimeError("Driver refuses non-stagenet subaddress destinations.")
            amount = int(invoice["Units"])
            if invoice["Case"] == "paid":
                amount += 5_000_000_000
            elif invoice["Case"] == "partial":
                amount //= 2
            if not 0 < amount <= 50_000_000_000:
                raise RuntimeError("Unexpected fixture payment size.")
            destinations.append({"address": invoice["Address"], "amount": amount})
        payment = rpc(spend_name, "transfer", {"destinations": destinations, "account_index": 0, "priority": 1, "unlock_time": 0, "get_tx_key": False})
        (run / "transaction.json").write_text(json.dumps({"txid": payment["tx_hash"], "fee": payment["fee"]}))
        print(json.dumps({"test_transaction": payment["tx_hash"], "cases": [entry["Case"] for entry in invoices], "run_directory": str(run)}), flush=True)
        wait_file("settled.json", time.monotonic() + 70 * 60)
        print("Funded settlement, replay and partial/late checks passed; restoring the view-only wallet.", flush=True)
        command(["docker", "stop", "--time", "20", view_name])
        view_key = rpc(spend_name, "query_key", {"key_type": "view_key"})["key"]
        command(["docker", "start", setup])
        time.sleep(2)
        restored = rpc(setup, "generate_from_keys", {"filename": "restored-view-" + suffix, "password": PASSWORD, "address": state["address"], "viewkey": view_key, "restore_height": state["restore_height"]})
        del view_key
        if restored.get("address") != state["address"]:
            raise RuntimeError("Restored view-only wallet has the wrong identity.")
        rpc(setup, "store", {})
        rpc(setup, "close_wallet", {})
        command(["docker", "stop", "--time", "20", setup])
        copy_wallet("restored-view-" + suffix, view)
        command(["docker", "start", view_name])
        ready = time.monotonic() + 90
        while True:
            try:
                rpc(view_name, "refresh", {}, tls=True)
                if rpc(view_name, "get_address", {"account_index": 0}, tls=True)["address"] == state["address"]:
                    break
            except (RuntimeError, ValueError):
                pass
            if time.monotonic() > ready:
                raise RuntimeError("Restored guarded wallet did not become ready.")
            time.sleep(2)
        (checkpoints / "restored.json").write_text('{"restored":true}')
        process.wait(timeout=120)
        log.close()
        print((run / "funded-check.log").read_text(), flush=True)
        if process.returncode != 0 or not (checkpoints / "passed.json").exists():
            raise RuntimeError("Funded test did not confirm all settlement/restore invariants.")
        print(json.dumps({"passed": True, "run_directory": str(run), "production_wallet_used": False}), flush=True)
    finally:
        if process is not None and process.poll() is None:
            process.terminate()
        for name in reversed(created):
            subprocess.run(["docker", "stop", "--time", "20", name], capture_output=True, timeout=30)
            subprocess.run(["docker", "rm", name], capture_output=True, timeout=30)
        subprocess.run(["docker", "stop", "--time", "20", setup], capture_output=True, timeout=30)
        if attached:
            subprocess.run(["docker", "network", "disconnect", private, "cygnet-payment-tests"], capture_output=True, timeout=30)
        for network in reversed(networks):
            subprocess.run(["docker", "network", "rm", network], capture_output=True, timeout=30)


if __name__ == "__main__":
    main()
