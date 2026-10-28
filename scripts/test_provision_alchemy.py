"""Offline tests for endpoint confinement, provider checks and private persistence."""

import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import Mock, patch
import urllib.error

spec = importlib.util.spec_from_file_location("provision_alchemy", Path(__file__).with_name("provision-alchemy.py"))
setup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(setup)


class AlchemySetupTests(unittest.TestCase):
    def test_rpc_rejects_bad_replies_without_exposing_urls(self):
        endpoint = "https://eth-mainnet.g.alchemy.com/v2/disposable-fixture"
        for scenario in ["ready", "oversized", "invalid json", "wrong id", "missing result", "rpc error", "http error", "connection error"]:
            with self.subTest(scenario=scenario):
                result = {"id": 1, "result": []}
                if scenario == "wrong id":
                    result["id"] = 2
                if scenario == "missing result":
                    del result["result"]
                if scenario == "rpc error":
                    result["error"] = {"code": -1, "message": endpoint}
                raw = json.dumps(result).encode()
                if scenario == "oversized":
                    raw = b"x" * (setup.LIMIT + 1)
                if scenario == "invalid json":
                    raw = b"invalid json"
                opener = Mock()
                opener.open.return_value = io.BytesIO(raw)
                if scenario == "http error":
                    opener.open.side_effect = urllib.error.HTTPError(endpoint, 403, endpoint, {}, None)
                if scenario == "connection error":
                    opener.open.side_effect = urllib.error.URLError(endpoint)
                with patch.object(setup.urllib.request, "build_opener", return_value=opener) as build:
                    if scenario == "ready":
                        self.assertEqual(setup.rpc(endpoint, "eth_chainId", []), [])
                    else:
                        with self.assertRaises(setup.SetupError) as failure:
                            setup.rpc(endpoint, "eth_chainId", [])
                        self.assertNotIn("disposable-fixture", str(failure.exception))
                    handlers = build.call_args.args
                    self.assertEqual(handlers[0].proxies, {})
                    self.assertIsInstance(handlers[1], setup.NoRedirect)

    def test_redirect_never_forwards_credentials(self):
        with self.assertRaises(setup.SetupError):
            setup.NoRedirect().redirect_request(None, None, 302, "redirect", {}, "https://elsewhere.example/v2/key")

    def test_endpoint_confinement(self):
        for network, host in setup.HOSTS.items():
            valid = "https://" + host + "/v2/disposable-fixture"
            self.assertEqual(setup.validate_endpoint(network, valid), valid)
            for bad in [valid.replace("https:", "http:"), valid + "/getNFTs", valid + "?key=other", valid + "#fragment",
                        valid + " ", valid + "\r", valid.replace(host, host + ".example.com"), valid.replace(host, "user@" + host),
                        valid.replace(host, host + ":443"), valid.replace(host, "127.0.0.1"), "https://" + host + "/v2/",
                        "https://" + host + "/v2/" + "x" * 257]:
                with self.subTest(network=network, bad=bad), self.assertRaises(setup.SetupError):
                    setup.validate_endpoint(network, bad)

    def test_provider_checks(self):
        for network, chain_id in [("ethereum", "0x1"), ("base", "0x2105")]:
            for scenario in ["ready", "wrong chain", "no block", "bad hash", "bad height", "zero height", "stale block", "future block", "missing transfers", "too many transfers"]:
                with self.subTest(network=network, scenario=scenario):
                    methods = []
                    def call(endpoint, method, params):
                        methods.append(method)
                        if method == "eth_chainId":
                            return "0x0" if scenario == "wrong chain" else chain_id
                        if method == "eth_getBlockByNumber":
                            self.assertEqual(params, ["finalized", False])
                            stamp = int(time.time()) + {"stale block": -7200, "future block": 600}.get(scenario, 0)
                            if scenario == "no block":
                                return None
                            return {"hash": "bad" if scenario == "bad hash" else "0x" + "a" * 64,
                                    "number": {"bad height": "0xnothex", "zero height": "0x0"}.get(scenario, "0x100"), "timestamp": hex(stamp)}
                        self.assertEqual(method, "alchemy_getAssetTransfers")
                        self.assertEqual(params[0]["category"], ["external", "internal"])
                        self.assertEqual(params[0]["fromBlock"], params[0]["toBlock"])
                        if scenario == "missing transfers":
                            return {}
                        return {"transfers": [{}, {}] if scenario == "too many transfers" else []}
                    with contextlib.redirect_stdout(io.StringIO()):
                        if scenario == "ready":
                            setup.check_network(network, "not-used-by-fixture", chain_id, call=call)
                            self.assertEqual(len(methods), 3)
                        else:
                            with self.assertRaises(setup.SetupError):
                                setup.check_network(network, "not-used-by-fixture", chain_id, call=call)

    def test_save_never_replaces_existing_values(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "alchemy.json"
            original = {"ethereum": "fixture-one", "base": "fixture-two"}
            setup.save_endpoints(target, original)
            self.assertEqual(json.loads(target.read_text()), original)
            with self.assertRaises(FileExistsError):
                setup.save_endpoints(target, {"ethereum": "replacement"})
            self.assertEqual(json.loads(target.read_text()), original)
            self.assertEqual([entry.name for entry in target.parent.iterdir()], ["alchemy.json"])


if __name__ == "__main__":
    unittest.main()
