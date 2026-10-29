package chain

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var hash32 = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

type nativeBlock struct {
	Number    string `json:"number"`
	Hash      string `json:"hash"`
	Timestamp string `json:"timestamp"`
}

// EVMNative discovers both ordinary and contract-originated ETH transfers
// using Alchemy's Transfers API. The ordinary RPC independently checks the
// chain, finalized boundary, successful receipt and canonical block.
// The indexer remains trusted for internal-transfer values and completeness.
// Cursor is next-block, or next-block|end-block|page for a bounded backlog.
// A page is returned only after all of its entries have been checked.
func (r *Reader) EVMNative(ctx context.Context, rail Rail, to, cursor string) ([]Transfer, string, error) {
	index := r.Native[rail.Chain]
	if !rail.Native || index == "" || !evmAddress.MatchString(to) {
		return nil, cursor, errors.New("native: rail is not configured")
	}
	endpoint := r.endpoint(rail)
	for _, target := range []string{endpoint, index} {
		var id string
		if err := r.rpc(ctx, target, "eth_chainId", nil, &id); err != nil {
			return nil, cursor, err
		}
		n, err := hexInt(id)
		if err != nil || !n.IsUint64() || n.Uint64() != rail.ChainID {
			return nil, cursor, errors.New("native: wrong chain")
		}
	}
	var head *nativeBlock
	if err := r.rpc(ctx, endpoint, "eth_getBlockByNumber", []any{"finalized", false}, &head); err != nil {
		return nil, cursor, err
	}
	if head == nil {
		return nil, cursor, errors.New("native: finalized head unavailable")
	}
	final, err := nativeHeight(head.Number)
	if err != nil {
		return nil, cursor, err
	}
	if cursor == "" {
		return nil, strconv.FormatUint(final+1, 10), nil
	}
	parts := strings.Split(cursor, "|")
	from, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || from == 0 || (len(parts) != 1 && len(parts) != 3) {
		return nil, cursor, errors.New("native: invalid cursor")
	}
	if from > final {
		return nil, cursor, nil
	}
	end := from + min(final-from, 999)
	page := ""
	if len(parts) == 3 {
		end, err = strconv.ParseUint(parts[1], 10, 64)
		if err != nil || end < from || end > final || end-from > 999 || len(parts[2]) > 1024 {
			return nil, cursor, errors.New("native: invalid page cursor")
		}
		page = parts[2]
	}
	filter := map[string]any{"fromBlock": fmt.Sprintf("0x%x", from), "toBlock": fmt.Sprintf("0x%x", end), "toAddress": to,
		"category": []string{"external", "internal"}, "excludeZeroValue": true, "withMetadata": false, "order": "asc", "maxCount": "0x32"}
	if page != "" {
		filter["pageKey"] = page
	}
	var result struct {
		Transfers []struct {
			Block    string `json:"blockNum"`
			ID       string `json:"uniqueId"`
			Hash     string `json:"hash"`
			From     string `json:"from"`
			To       string `json:"to"`
			Asset    string `json:"asset"`
			Category string `json:"category"`
			Raw      struct {
				Value   string  `json:"value"`
				Address *string `json:"address"`
				Decimal string  `json:"decimal"`
			} `json:"rawContract"`
		} `json:"transfers"`
		Page string `json:"pageKey"`
	}
	if err := r.rpc(ctx, index, "alchemy_getAssetTransfers", []any{filter}, &result); err != nil {
		// Index pagination tokens expire. Replay this range after an error
		// instead of trapping the watcher on a dead token after downtime.
		// Stable event identities make that replay harmless.
		if page != "" {
			return nil, strconv.FormatUint(from, 10), err
		}
		return nil, cursor, err
	}
	if len(result.Transfers) > 50 || len(result.Page) > 1024 || strings.Contains(result.Page, "|") || (result.Page != "" && result.Page == page) {
		return nil, cursor, errors.New("native: invalid transfer page")
	}
	var out []Transfer
	blocks := map[uint64]*nativeBlock{}
	seen := map[string]bool{}
	for _, t := range result.Transfers {
		at, err := nativeHeight(t.Block)
		if err != nil || at < from || at > end || !hash32.MatchString(t.Hash) || !strings.EqualFold(t.To, to) || !evmAddress.MatchString(t.From) ||
			t.Asset != "ETH" || (t.Category != "external" && t.Category != "internal") || t.Raw.Address != nil || t.Raw.Decimal != "0x12" ||
			!strings.HasPrefix(strings.ToLower(t.ID), strings.ToLower(t.Hash)+":") || len(t.ID) > 256 {
			return nil, cursor, errors.New("native: invalid indexed transfer")
		}
		amount, err := hexInt(t.Raw.Value)
		if err != nil || amount.Sign() <= 0 || amount.BitLen() > 256 {
			return nil, cursor, errors.New("native: invalid amount")
		}
		identity := strings.ToLower(t.ID)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		var receipt *struct {
			Status    string `json:"status"`
			Block     string `json:"blockNumber"`
			BlockHash string `json:"blockHash"`
			Hash      string `json:"transactionHash"`
		}
		if err := r.rpc(ctx, endpoint, "eth_getTransactionReceipt", []any{t.Hash}, &receipt); err != nil {
			return nil, cursor, err
		}
		if receipt == nil {
			return nil, cursor, errors.New("native: receipt unavailable")
		}
		if receipt.Status == "0x0" {
			continue
		}
		rh, err := nativeHeight(receipt.Block)
		if err != nil || rh != at || receipt.Status != "0x1" || !strings.EqualFold(receipt.Hash, t.Hash) {
			return nil, cursor, errors.New("native: invalid receipt")
		}
		block := blocks[at]
		if block == nil {
			if err := r.rpc(ctx, endpoint, "eth_getBlockByNumber", []any{t.Block, false}, &block); err != nil {
				return nil, cursor, err
			}
			if block == nil {
				return nil, cursor, errors.New("native: block unavailable")
			}
			blocks[at] = block
		}
		bh, err := nativeHeight(block.Number)
		if err != nil || bh != at || !hash32.MatchString(block.Hash) || !strings.EqualFold(block.Hash, receipt.BlockHash) {
			return nil, cursor, errors.New("native: receipt is not canonical")
		}
		stamp, err := nativeHeight(block.Timestamp)
		if err != nil || stamp == 0 || stamp > 1<<62 {
			return nil, cursor, errors.New("native: invalid block time")
		}
		out = append(out, Transfer{Tx: strings.ToLower(t.Hash), Event: identity, To: strings.ToLower(to), Amount: amount, From: strings.ToLower(t.From), At: time.Unix(int64(stamp), 0)})
	}
	next := strconv.FormatUint(end+1, 10)
	if result.Page != "" {
		next = fmt.Sprintf("%d|%d|%s", from, end, result.Page)
	}
	return out, next, nil
}

func nativeHeight(raw string) (uint64, error) {
	n, err := hexInt(raw)
	if err != nil || !n.IsUint64() || n.Uint64() == ^uint64(0) {
		return 0, errors.New("native: invalid block quantity")
	}
	return n.Uint64(), nil
}
