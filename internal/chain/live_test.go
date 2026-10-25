package chain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The live tests read the real chains through the public endpoints the
// rails name: each finds a recent real transfer and checks the reader
// sees it, so the answers' shape is the chain's and not a fake's. Only
// reads; run them with CYGNET_LIVE_CHAINS=1 before changing a reader.
func live(t *testing.T) (*Reader, context.Context) {
	t.Helper()
	if os.Getenv("CYGNET_LIVE_CHAINS") == "" {
		t.Skip("CYGNET_LIVE_CHAINS is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return &Reader{}, ctx
}

func TestLiveEVM(t *testing.T) {
	r, ctx := live(t)
	for _, key := range []string{"base:USDC", "polygon:USDC"} {
		t.Run(key, func(t *testing.T) {
			rl := rail(t, key)
			var head string
			if err := r.rpc(ctx, rl.RPC, "eth_blockNumber", []any{}, &head); err != nil {
				t.Fatal(err)
			}
			h, _ := hexInt(head)
			block := h.Uint64() - rl.Confirmations - 5
			var logs []struct {
				Topics []string `json:"topics"`
			}
			at := "0x" + strconv.FormatUint(block, 16)
			if err := r.rpc(ctx, rl.RPC, "eth_getLogs", []any{map[string]any{"fromBlock": at, "toBlock": at, "address": rl.Token, "topics": []any{transferTopic}}}, &logs); err != nil {
				t.Fatal(err)
			}
			if len(logs) == 0 {
				t.Skip("no transfer in that block")
			}
			to := "0x" + logs[0].Topics[2][26:]
			got, next, err := r.EVM(ctx, rl, to, block)
			if err != nil || next <= block || len(got) == 0 || got[0].Amount.Sign() <= 0 {
				t.Fatalf("%s from block %d: %+v next %d %v", to, block, got, next, err)
			}
			t.Logf("%d transfers to %s, first %s %s %s", len(got), to, got[0].Tx, rl.Format(got[0].Amount), got[0].From)
		})
	}
}

func TestLiveTron(t *testing.T) {
	r, ctx := live(t)
	rl := rail(t, "tron:USDT")
	// A recent transfer to anyone, then that wallet's transfers from its time.
	raw, err := r.do(ctx, "GET", rl.RPC+"/v1/contracts/"+rl.Token+"/events?event_name=Transfer&only_confirmed=true&limit=1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ev struct {
		Data []struct {
			Time   int64             `json:"block_timestamp"`
			Result map[string]string `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil || len(ev.Data) == 0 {
		t.Fatalf("events: %v %s", err, raw[:min(200, len(raw))])
	}
	hexTo := ev.Data[0].Result["to"]
	to := tronBase58(t, hexTo)
	got, next, err := r.Tron(ctx, rl, to, ev.Data[0].Time, time.Now())
	if err != nil || len(got) == 0 || next < ev.Data[0].Time {
		t.Fatalf("%s from %d: %+v next %d %v", to, ev.Data[0].Time, got, next, err)
	}
	t.Logf("%d transfers to %s, first %s %s USDT from %s", len(got), to, got[0].Tx, rl.Format(got[0].Amount), got[0].From)
}

// tronBase58 is a TRON address from the hex the events endpoint gives
// (0x and 20 bytes), with its checksum: what TronGrid's lists take.
func tronBase58(t *testing.T, h string) string {
	t.Helper()
	b, err := hex.DecodeString(strings.TrimPrefix(h, "0x"))
	if err != nil || len(b) != 20 {
		t.Fatalf("%q is not a tron hex address", h)
	}
	payload := append([]byte{0x41}, b...)
	first := sha256.Sum256(payload)
	second := sha256.Sum256(first[:])
	full := append(payload, second[:4]...)
	n := new(big.Int).SetBytes(full)
	var out []byte
	for n.Sign() > 0 {
		m := new(big.Int)
		n.DivMod(n, big.NewInt(58), m)
		out = append([]byte{base58Alphabet[m.Int64()]}, out...)
	}
	addr := string(out)
	if _, err := tronHex(addr); err != nil {
		t.Fatalf("encoded %s: %v", addr, err)
	}
	return addr
}

func TestLiveSolana(t *testing.T) {
	r, ctx := live(t)
	rl := rail(t, "solana:USDC")
	var sigs []struct {
		Signature string `json:"signature"`
		Err       any    `json:"err"`
	}
	if err := r.rpc(ctx, rl.RPC, "getSignaturesForAddress", []any{rl.Token, map[string]any{"limit": 20, "commitment": "finalized"}}, &sigs); err != nil {
		t.Fatal(err)
	}
	for _, s := range sigs {
		if s.Err != nil {
			continue
		}
		var tx struct {
			Meta struct {
				Post []struct {
					Index int    `json:"accountIndex"`
					Mint  string `json:"mint"`
					Owner string `json:"owner"`
				} `json:"postTokenBalances"`
			} `json:"meta"`
			Transaction struct {
				Message struct {
					Keys []struct {
						Pubkey string `json:"pubkey"`
					} `json:"accountKeys"`
				} `json:"message"`
			} `json:"transaction"`
		}
		if err := r.rpc(ctx, rl.RPC, "getTransaction", []any{s.Signature, map[string]any{"encoding": "jsonParsed", "commitment": "finalized", "maxSupportedTransactionVersion": 0}}, &tx); err != nil {
			t.Fatal(err)
		}
		for _, b := range tx.Meta.Post {
			if b.Mint != rl.Token || b.Owner == "" {
				continue
			}
			t1, err := r.received(ctx, rl.RPC, rl, b.Owner, s.Signature)
			if err != nil {
				t.Fatal(err)
			}
			if t1 != nil {
				t.Logf("%s received %s USDC in %s from %s", b.Owner, rl.Format(t1.Amount), s.Signature, t1.From)
				accts, err := r.Accounts(ctx, rl, b.Owner)
				if err != nil || len(accts) == 0 {
					t.Fatalf("accounts of %s: %v %v", b.Owner, accts, err)
				}
				if !strings.Contains(strings.Join(accts, " "), tx.Transaction.Message.Keys[b.Index].Pubkey) {
					t.Fatalf("%s's accounts %v miss %s", b.Owner, accts, tx.Transaction.Message.Keys[b.Index].Pubkey)
				}
				return
			}
		}
	}
	t.Skip("no incoming USDC in the last few transactions")
}

func TestLivePrice(t *testing.T) {
	r, ctx := live(t)
	p, err := r.USD(ctx, "SOL")
	if err != nil {
		t.Fatal(err)
	}
	base, err := rail(t, "solana:SOL").Base(500, p)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SOL is $%s; $5 is %s SOL", p.FloatString(2), rail(t, "solana:SOL").Format(base))
}
