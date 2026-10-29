package chain

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const nativeHash = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const blockHash = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func nativeFixture(t *testing.T) (*Reader, map[string]func([]json.RawMessage) any, map[string]any, map[string]any, map[string]any) {
	t.Helper()
	transfer := map[string]any{"blockNum": "0x10", "uniqueId": nativeHash + ":external", "hash": nativeHash, "from": wallet, "to": wallet,
		"asset": "ETH", "category": "external", "rawContract": map[string]any{"value": "0x10000000000001", "address": nil, "decimal": "0x12"}}
	result := map[string]any{"transfers": []any{transfer}}
	receipt := map[string]any{"status": "0x1", "blockNumber": "0x10", "blockHash": blockHash, "transactionHash": nativeHash}
	methods := map[string]func([]json.RawMessage) any{
		"eth_chainId": func([]json.RawMessage) any { return "0x2105" },
		"eth_getBlockByNumber": func(p []json.RawMessage) any {
			var block string
			_ = json.Unmarshal(p[0], &block)
			if block == "finalized" {
				return map[string]any{"number": "0x20"}
			}
			return map[string]any{"number": "0x10", "hash": blockHash, "timestamp": "0x6553f100"}
		},
		"eth_getTransactionReceipt": func([]json.RawMessage) any { return receipt },
		"eth_getTransactionByHash": func([]json.RawMessage) any {
			return map[string]any{"hash": nativeHash, "from": wallet, "to": wallet, "value": "0x10000000000001", "blockNumber": "0x10", "blockHash": blockHash}
		},
		"alchemy_getAssetTransfers": func(p []json.RawMessage) any {
			var filter map[string]any
			_ = json.Unmarshal(p[0], &filter)
			if filter["toAddress"] != wallet || filter["order"] != "asc" || filter["maxCount"] != "0x32" {
				t.Errorf("unsafe filter: %v", filter)
			}
			return result
		},
	}
	srv, _ := node(t, methods)
	return &Reader{Native: map[string]string{"base": srv.URL}, RPC: map[string]string{"base": srv.URL}}, methods, transfer, result, receipt
}

func TestNativeETHRequiresFinalizedCanonicalSuccessfulReceipts(t *testing.T) {
	r, _, transfer, result, _ := nativeFixture(t)
	rail := rail(t, "base:ETH")
	got, cursor, err := r.EVMNative(context.Background(), rail, wallet, "")
	if err != nil || len(got) != 0 || cursor != "33" {
		t.Fatalf("initial cursor: %v %s %v", got, cursor, err)
	}
	got, cursor, err = r.EVMNative(context.Background(), rail, wallet, "16")
	if err != nil || len(got) != 1 || cursor != "33" || got[0].Amount.String() != "4503599627370497" || got[0].Event != nativeHash+":external" || got[0].At.Unix() != 1700000000 {
		t.Fatalf("receipt: %+v %s %v", got, cursor, err)
	}
	// A contract send uses its own stable event identity, without rounding.
	transfer["category"] = "internal"
	transfer["uniqueId"] = nativeHash + ":internal:0"
	result["transfers"] = []any{transfer, transfer}
	result["pageKey"] = "next-page"
	got, cursor, err = r.EVMNative(context.Background(), rail, wallet, "16")
	if err != nil || len(got) != 1 || cursor != "16|32|next-page" {
		t.Fatalf("internal/page: %+v %s %v", got, cursor, err)
	}
	delete(result, "pageKey")
	_, cursor, err = r.EVMNative(context.Background(), rail, wallet, cursor)
	if err != nil || cursor != "33" {
		t.Fatalf("resume: %s %v", cursor, err)
	}
	got, cursor, err = r.EVMNative(context.Background(), rail, wallet, "33")
	if err != nil || len(got) != 0 || cursor != "33" {
		t.Fatalf("caught up: %s %v", cursor, err)
	}
}

func TestExpiredNativePaginationReplaysTheRange(t *testing.T) {
	r, methods, _, _, _ := nativeFixture(t)
	delete(methods, "alchemy_getAssetTransfers")
	got, cursor, err := r.EVMNative(context.Background(), rail(t, "base:ETH"), wallet, "16|32|expired")
	if err == nil || len(got) != 0 || cursor != "16" {
		t.Fatalf("expired page did not reset safely: %+v %s %v", got, cursor, err)
	}
}

func TestNativeExternalTransferRequiresMatchingTransaction(t *testing.T) {
	for _, scenario := range []string{"missing", "rpc error", "wrong hash", "wrong recipient", "wrong sender", "wrong block hash", "wrong block height", "bad height", "wrong value", "bad value", "oversized value", "contract creation"} {
		t.Run(scenario, func(t *testing.T) {
			r, methods, _, _, _ := nativeFixture(t)
			if scenario == "rpc error" {
				delete(methods, "eth_getTransactionByHash")
			} else {
				methods["eth_getTransactionByHash"] = func([]json.RawMessage) any {
					if scenario == "missing" {
						return nil
					}
					tx := map[string]any{"hash": nativeHash, "from": wallet, "to": wallet, "value": "0x10000000000001", "blockNumber": "0x10", "blockHash": blockHash}
					switch scenario {
					case "wrong hash":
						tx["hash"] = blockHash
					case "wrong recipient":
						tx["to"] = "0x2222222222222222222222222222222222222222"
					case "wrong sender":
						tx["from"] = "0x2222222222222222222222222222222222222222"
					case "wrong block hash":
						tx["blockHash"] = nativeHash
					case "wrong block height":
						tx["blockNumber"] = "0x11"
					case "bad height":
						tx["blockNumber"] = "not hex"
					case "wrong value":
						tx["value"] = "0x1"
					case "bad value":
						tx["value"] = "not hex"
					case "oversized value":
						tx["value"] = "0x" + strings.Repeat("f", 65)
					case "contract creation":
						tx["to"] = nil
					}
					return tx
				}
			}
			got, cursor, err := r.EVMNative(context.Background(), rail(t, "base:ETH"), wallet, "16")
			if err == nil || len(got) != 0 || cursor != "16" {
				t.Fatalf("unproven transfer accepted or cursor advanced: %v %q %v", got, cursor, err)
			}
		})
	}
}

func TestNativeETHRejectsMalformedOrUnverifiedDataWithoutAdvancing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]func([]json.RawMessage) any, map[string]any, map[string]any, map[string]any)
	}{
		{"wrong chain", func(m map[string]func([]json.RawMessage) any, _, _, _ map[string]any) {
			m["eth_chainId"] = func([]json.RawMessage) any { return "0x1" }
		}},
		{"no head", func(m map[string]func([]json.RawMessage) any, _, _, _ map[string]any) {
			m["eth_getBlockByNumber"] = func([]json.RawMessage) any { return nil }
		}},
		{"index down", func(m map[string]func([]json.RawMessage) any, _, _, _ map[string]any) {
			delete(m, "alchemy_getAssetTransfers")
		}},
		{"wrong recipient", func(_ map[string]func([]json.RawMessage) any, x, _, _ map[string]any) {
			x["to"] = "0x2222222222222222222222222222222222222222"
		}},
		{"not native", func(_ map[string]func([]json.RawMessage) any, x, _, _ map[string]any) { x["asset"] = "WETH" }},
		{"wrong category", func(_ map[string]func([]json.RawMessage) any, x, _, _ map[string]any) { x["category"] = "erc20" }},
		{"float amount", func(_ map[string]func([]json.RawMessage) any, x, _, _ map[string]any) {
			x["rawContract"] = map[string]any{"value": "0.1", "decimal": "0x12"}
		}},
		{"zero amount", func(_ map[string]func([]json.RawMessage) any, x, _, _ map[string]any) {
			x["rawContract"] = map[string]any{"value": "0x0", "decimal": "0x12"}
		}},
		{"wrong block", func(_ map[string]func([]json.RawMessage) any, x, _, _ map[string]any) { x["blockNum"] = "0x30" }},
		{"bad identity", func(_ map[string]func([]json.RawMessage) any, x, _, _ map[string]any) {
			x["uniqueId"] = "not a transaction identity"
		}},
		{"no receipt", func(m map[string]func([]json.RawMessage) any, _, _, _ map[string]any) {
			m["eth_getTransactionReceipt"] = func([]json.RawMessage) any { return nil }
		}},
		{"receipt down", func(m map[string]func([]json.RawMessage) any, _, _, _ map[string]any) {
			delete(m, "eth_getTransactionReceipt")
		}},
		{"receipt block", func(_ map[string]func([]json.RawMessage) any, _, _, x map[string]any) { x["blockNumber"] = "0x11" }},
		{"receipt hash", func(_ map[string]func([]json.RawMessage) any, _, _, x map[string]any) {
			x["transactionHash"] = blockHash
		}},
		{"reorg", func(_ map[string]func([]json.RawMessage) any, _, _, x map[string]any) { x["blockHash"] = nativeHash }},
		{"page cycle", func(_ map[string]func([]json.RawMessage) any, _, x, _ map[string]any) { x["pageKey"] = "a|b" }},
		{"too many", func(_ map[string]func([]json.RawMessage) any, x, result, _ map[string]any) {
			all := make([]any, 51)
			for i := range all {
				all[i] = x
			}
			result["transfers"] = all
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, methods, transfer, result, receipt := nativeFixture(t)
			tc.change(methods, transfer, result, receipt)
			got, cursor, err := r.EVMNative(context.Background(), rail(t, "base:ETH"), wallet, "16")
			if err == nil || len(got) != 0 || cursor != "16" {
				t.Fatalf("accepted broken data: %+v %s %v", got, cursor, err)
			}
		})
	}
	r, _, _, _, receipt := nativeFixture(t)
	receipt["status"] = "0x0"
	got, _, err := r.EVMNative(context.Background(), rail(t, "base:ETH"), wallet, "16")
	if err != nil || len(got) != 0 {
		t.Fatalf("failed transfer credited: %+v %v", got, err)
	}
	for _, cursor := range []string{"no", "0", "1|2", "1|0|x", "1|10000|x", "1|32|" + strings.Repeat("x", 1025)} {
		if _, _, err := r.EVMNative(context.Background(), rail(t, "base:ETH"), wallet, cursor); err == nil {
			t.Errorf("accepted cursor %q", cursor)
		}
	}
	if _, _, err := r.EVMNative(context.Background(), rail(t, "base:ETH"), "bad", ""); err == nil {
		t.Fatal("accepted bad wallet")
	}
	for _, raw := range []string{"", "0xno", "0xffffffffffffffff", "0x10000000000000000"} {
		if _, err := nativeHeight(raw); err == nil {
			t.Errorf("accepted quantity %q", raw)
		}
	}
}
