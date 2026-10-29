#!/usr/bin/env python3
"""Validate the prepared wallet in temporary isolated containers; never activate checkout."""

import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import time


def command(args, timeout=30):
    return subprocess.check_output(args, text=True, timeout=timeout, stderr=subprocess.DEVNULL).strip()


def private_path(path, owner, mode, directory=False):
    metadata = path.lstat()
    expected_type = stat.S_ISDIR if directory else stat.S_ISREG
    if not expected_type(metadata.st_mode) or metadata.st_uid != owner or metadata.st_gid != owner or stat.S_IMODE(metadata.st_mode) != mode:
        raise RuntimeError("Unexpected ownership, permissions or type for a required runtime path.")


def main():
    if os.geteuid() != 0:
        raise RuntimeError("Run with sudo.")
    os.umask(0o077)
    runtime = Path("/var/lib/cygnet-monero-runtime")
    private_path(runtime, 0, 0o700, directory=True)
    base = Path((runtime / "project-dir").read_text().strip())
    if str(base) != "/home/deploy/cygnet" or base.resolve() != base:
        raise RuntimeError("Unexpected deployment directory.")
    activation = runtime / "activation.env"
    private_path(activation, 0, 0o600)
    values = {}
    for line in activation.read_text().splitlines():
        key, separator, value = line.partition("=")
        if not separator or key in values:
            raise RuntimeError("Invalid activation metadata.")
        values[key] = value
    fixed = {
        "CYGNET_XMR_RPC": "https://xmr-wallet:18088/json_rpc",
        "CYGNET_XMR_RPC_USER": "cygnet",
        "CYGNET_XMR_RPC_PASSWORD_FILE": "/run/secrets/xmr-rpc-password",
        "CYGNET_XMR_RPC_CA_FILE": "/run/secrets/xmr-rpc-ca.crt",
        "CYGNET_XMR_NETWORK": "Mainnet",
    }
    if set(values) != set(fixed) | {"CYGNET_XMR_IMAGE", "CYGNET_SUPPORT_MONERO"} or any(values[key] != value for key, value in fixed.items()):
        raise RuntimeError("Unexpected activation settings.")
    image = (runtime / "image-ref").read_text().strip()
    if values["CYGNET_XMR_IMAGE"] != image or not image.startswith("sha256:") or len(image) != 71:
        raise RuntimeError("Unexpected immutable image identity.")
    image_info = json.loads(command(["docker", "image", "inspect", image]))[0]
    if image_info["Id"] != image or image_info["Architecture"] != "arm64" or image_info["Config"]["User"] != "65532:65532" or image_info["Config"]["Entrypoint"] != ["/usr/local/bin/monero-guard"]:
        raise RuntimeError("Unexpected wallet runtime image.")
    checker = runtime / "monero-check"
    private_path(checker, 0, 0o555)
    private_path(base / "secrets", 65532, 0o700, directory=True)
    private_path(base / "xmr-wallet", 65532, 0o700, directory=True)
    for filename in ["xmr-rpc-password", "xmr-wallet-password", "xmr-wallet.conf", "xmr-rpc-tls.key"]:
        private_path(base / "secrets" / filename, 65532, 0o400)
    private_path(base / "secrets/xmr-rpc-ca.crt", 65532, 0o444)
    for filename in ["support-view", "support-view.keys"]:
        private_path(base / "xmr-wallet" / filename, 65532, 0o600)

    bots = ["cygnet-bot-1", "merlin-bot-1"]
    def snapshot():
        return [command(["docker", "inspect", "--format", "{{.Id}} {{.Image}} {{.State.StartedAt}}", bot]) for bot in bots]
    before = snapshot()
    # Unique names and only objects created below are eligible for cleanup.
    suffix = os.urandom(6).hex()
    name = "cygnet-xmr-preflight-" + suffix
    private = name + "-private"
    egress = name + "-egress"
    networks = []
    created = False
    protections = ["--read-only", "--user", "65532:65532", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--pids-limit", "64", "--cpus", "1", "--log-driver", "none", "--pull", "never"]
    try:
        for network, extra in [(private, ["--internal"]), (egress, [])]:
            command(["docker", "network", "create", "--label", "codex.task=cygnet-xmr-preflight", *extra, network])
            networks.append(network)
        args = ["docker", "create", "--name", name, "--label", "codex.task=cygnet-xmr-preflight", *protections,
                "--network", private, "--network-alias", "xmr-wallet", "--memory", "512m", "--tmpfs", "/tmp:size=16m,mode=1777",
                "--env", "CYGNET_XMR_PRIMARY=" + values["CYGNET_SUPPORT_MONERO"], "--env", "CYGNET_XMR_NETWORK=Mainnet"]
        args += ["--mount", f"type=bind,source={base}/xmr-wallet,target=/wallet"]
        for filename in ["xmr-wallet.conf", "xmr-wallet-password", "xmr-rpc-tls.key", "xmr-rpc-ca.crt"]:
            args += ["--mount", f"type=bind,source={base}/secrets/{filename},target=/run/secrets/{filename},readonly"]
        command([*args, image])
        created = True
        command(["docker", "network", "connect", egress, name])
        command(["docker", "start", name])
        info = json.loads(command(["docker", "inspect", name]))[0]
        if info["HostConfig"]["PortBindings"] or set(info["NetworkSettings"]["Networks"]) != {private, egress}:
            raise RuntimeError("Wallet isolation check failed.")
        args = ["docker", "run", "--rm", *protections, "--network", private, "--memory", "64m", "--tmpfs", "/tmp:size=1m,mode=1777",
                "--mount", f"type=bind,source={checker},target=/monero-check,readonly", "--entrypoint", "/monero-check"]
        for filename in ["xmr-rpc-password", "xmr-rpc-ca.crt"]:
            args += ["--mount", f"type=bind,source={base}/secrets/{filename},target=/run/secrets/{filename},readonly"]
        for key, value in values.items():
            if key != "CYGNET_XMR_IMAGE":
                args += ["--env", key + "=" + value]
        deadline = time.monotonic() + 90
        last_error = "no readiness result"
        reported = set()
        while True:
            try:
                attempt = subprocess.run([*args, image], text=True, capture_output=True, timeout=40)
                if attempt.returncode != 0:
                    safe_errors = {"wallet check: cannot read RPC password file", "wallet check: cannot read pinned certificate",
                                   "monero: invalid private wallet configuration", "monero: invalid private TLS certificate",
                                   "monero: private wallet unavailable", "monero: wallet does not match its pinned address",
                                   "monero: wallet RPC authentication is disabled", "monero: wallet RPC refused the operation",
                                   "monero: pinned wallet is not synchronized", "monero: wallet answered 401"}
                    if attempt.stderr.strip() in safe_errors:
                        last_error = attempt.stderr.strip()
                    else:
                        last_error = "checker execution failed; raw output suppressed"
                    if last_error not in reported:
                        print("Readiness pending: " + last_error, flush=True)
                        reported.add(last_error)
                    raise subprocess.CalledProcessError(attempt.returncode, "wallet readiness check")
                result = attempt.stdout.strip()
                break
            except subprocess.CalledProcessError:
                state = command(["docker", "inspect", "--format", "{{.State.Status}} {{.State.ExitCode}}", name])
                if state != "running 0":
                    raise RuntimeError("Guarded wallet stopped during startup (" + state + ").") from None
                if time.monotonic() >= deadline:
                    raise RuntimeError("Cygnet readiness check did not succeed: " + last_error + ".") from None
                time.sleep(2)
        if result != "Wallet identity, verified TLS, RPC authentication and synchronization passed.":
            raise RuntimeError("Unexpected readiness checker result.")
        print(result, flush=True)
    finally:
        if created:
            command(["docker", "stop", "--time", "20", name], timeout=30)
            command(["docker", "rm", name])
        for network in reversed(networks):
            command(["docker", "network", "rm", network])
        if snapshot() != before:
            raise RuntimeError("An existing bot changed during preflight.")
    print(json.dumps({"bots_unchanged": True, "checkout_enabled": False, "temporary_wallet_stopped": True,
                      "checker_sha256": hashlib.sha256(checker.read_bytes()).hexdigest()}))


if __name__ == "__main__":
    try:
        main()
    except subprocess.SubprocessError:
        raise SystemExit("Wallet preflight command failed; credentials and raw wallet output were not collected.") from None
    except RuntimeError as error:
        raise SystemExit(str(error) + " No activation was attempted.") from None
    except (OSError, ValueError, KeyError) as error:
        # Paths and exception strings may contain wallet metadata; expose no raw errors.
        raise SystemExit("Wallet preflight failed (" + type(error).__name__ + "); no activation was attempted.") from None
