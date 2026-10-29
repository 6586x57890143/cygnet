#!/usr/bin/env python3
"""Collect and validate Alchemy endpoints privately; never activate payments."""

import getpass
import json
import os
from pathlib import Path
import re
import ssl
import stat
import sys
import tempfile
import time
import urllib.error
import urllib.request
import warnings

sys.dont_write_bytecode = True
LIMIT = 1 << 20
HOSTS = {"ethereum": "eth-mainnet.g.alchemy.com", "base": "base-mainnet.g.alchemy.com"}


class SetupError(Exception):
    """Only fixed, non-secret messages may be raised through this exception."""


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, file, code, message, headers, new_url):
        raise SetupError("Endpoint redirected; refusing to forward the API key.")


def validate_endpoint(network, value):
    if not re.fullmatch(r"https://" + re.escape(HOSTS[network]) + r"/v2/[A-Za-z0-9_-]{1,256}", value):
        raise SetupError("Paste the full HTTPS mainnet RPC URL for the requested network, with no spaces, query or extra path.")
    return value


def rpc(endpoint, method, params):
    context = ssl.create_default_context()
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect(), urllib.request.HTTPSHandler(context=context))
    request = urllib.request.Request(endpoint, data=json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode(),
                                     headers={"Content-Type": "application/json"})
    try:
        with opener.open(request, timeout=15) as response:
            raw = response.read(LIMIT + 1)
        if len(raw) > LIMIT:
            raise SetupError("Provider response exceeded the size limit.")
        reply = json.loads(raw)
    except urllib.error.HTTPError as error:
        raise SetupError("Provider rejected the request (HTTP " + str(error.code) + "). Check the key and enabled mainnet networks.") from None
    except (OSError, ValueError, urllib.error.URLError):
        raise SetupError("Provider connection or response validation failed; no raw error was displayed.") from None
    if not isinstance(reply, dict) or reply.get("id") != 1 or "error" in reply or reply.get("result") is None:
        raise SetupError("Provider refused the RPC method or returned an invalid reply. Check Node API and Transfers API access.")
    return reply["result"]


def check_network(network, endpoint, chain_id, call=rpc):
    if call(endpoint, "eth_chainId", []) != chain_id:
        raise SetupError("Endpoint is on the wrong chain.")
    block = call(endpoint, "eth_getBlockByNumber", ["finalized", False])
    if not isinstance(block, dict) or not re.fullmatch(r"0x[0-9a-fA-F]{64}", str(block.get("hash"))):
        raise SetupError("Provider did not return a valid finalized block.")
    for field in ["number", "timestamp"]:
        if not re.fullmatch(r"0x[0-9a-fA-F]{1,16}", str(block.get(field))) or int(block[field], 16) == 0:
            raise SetupError("Provider returned an invalid finalized block quantity.")
    if not -120 <= time.time() - int(block["timestamp"], 16) <= 3600:
        raise SetupError("Finalized block is too old or has a future timestamp.")
    # One finalized block and one public burn address: no personal wallet data.
    transfers = call(endpoint, "alchemy_getAssetTransfers", [{"fromBlock": block["number"], "toBlock": block["number"],
                     "toAddress": "0x000000000000000000000000000000000000dEaD", "category": ["external", "internal"],
                     "excludeZeroValue": True, "withMetadata": False, "order": "asc", "maxCount": "0x1"}])
    if not isinstance(transfers, dict) or not isinstance(transfers.get("transfers"), list) or len(transfers["transfers"]) > 1:
        raise SetupError("Provider returned an invalid transfer-discovery response.")
    print(network.capitalize() + ": correct mainnet, finalized block and external/internal Transfers API checks passed.", flush=True)


def save_endpoints(target, values):
    descriptor, temporary = tempfile.mkstemp(prefix=".alchemy-", dir=target.parent)
    try:
        with os.fdopen(descriptor, "w") as stream:
            json.dump(values, stream)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        # Atomic publication without replacing an existing file, including symlinks.
        os.link(temporary, target)
    finally:
        os.unlink(temporary)


def main():
    if not sys.stdin.isatty() or not sys.stdout.isatty():
        raise SetupError("Run from your own interactive SSH terminal.")
    if os.geteuid() != 0:
        raise SetupError("Run with sudo.")
    import resource
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    os.umask(0o077)
    warnings.simplefilter("error", getpass.GetPassWarning)
    runtime = Path("/var/lib/cygnet-payment-runtime")
    if not runtime.exists() and not runtime.is_symlink():
        runtime.mkdir(mode=0o700)
    metadata = runtime.lstat()
    if not stat.S_ISDIR(metadata.st_mode) or metadata.st_uid != 0 or metadata.st_gid != 0 or stat.S_IMODE(metadata.st_mode) != 0o700:
        raise SetupError("Unexpected runtime directory ownership or permissions.")
    target = runtime / "alchemy.json"
    if target.exists() or target.is_symlink():
        raise SetupError("Alchemy endpoints are already prepared; refusing to overwrite them.")
    values = {}
    for network, chain_id in [("ethereum", "0x1"), ("base", "0x2105")]:
        endpoint = validate_endpoint(network, getpass.getpass(network.capitalize() + " Mainnet HTTPS RPC URL (hidden): "))
        check_network(network, endpoint, chain_id)
        values[network] = endpoint
    save_endpoints(target, values)
    print("Both validated endpoints are saved in a root-only file. No active environment, service or checkout was changed.")


if __name__ == "__main__":
    try:
        main()
    except SetupError as error:
        raise SystemExit(str(error)) from None
    except (Exception, KeyboardInterrupt):
        # URLs embed API keys, so never expose library errors or tracebacks.
        raise SystemExit("Setup stopped; no endpoint values or raw errors were displayed. Payments were not enabled.") from None
