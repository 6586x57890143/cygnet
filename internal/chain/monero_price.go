package chain

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// Kraken's recent-trades endpoint carries an exchange trade timestamp.
// A stale feed never silently sells backing at an old XMR price.
func (r *Reader) moneroUSD(ctx context.Context, now time.Time) (*big.Rat, error) {
	base := r.Kraken
	if base == "" {
		base = "https://api.kraken.com"
	}
	raw, err := r.do(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/0/public/Trades?pair=XMRUSD&count=1", nil, nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Error  []string                   `json:"error"`
		Result map[string]json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Error) != 0 {
		return nil, errors.New("price: XMR feed refused the quote")
	}
	rows := response.Result["XXMRZUSD"]
	if rows == nil {
		rows = response.Result["XMRUSD"]
	}
	var trades [][]json.RawMessage
	if json.Unmarshal(rows, &trades) != nil || len(trades) != 1 || len(trades[0]) < 3 {
		return nil, errors.New("price: invalid XMR trade")
	}
	var price string
	if json.Unmarshal(trades[0][0], &price) != nil || len(price) > 48 || !decimal.MatchString(price) {
		return nil, errors.New("price: invalid XMR price")
	}
	value, ok := new(big.Rat).SetString(price)
	if !ok || value.Cmp(big.NewRat(1, 1)) < 0 || value.Cmp(big.NewRat(10000, 1)) > 0 {
		return nil, errors.New("price: XMR price outside bounds")
	}
	if len(trades[0][2]) > 32 || !decimal.Match(trades[0][2]) {
		return nil, errors.New("price: invalid XMR trade time")
	}
	stamp, ok := new(big.Rat).SetString(string(trades[0][2]))
	if !ok {
		return nil, errors.New("price: invalid XMR trade time")
	}
	stamp.Mul(stamp, big.NewRat(1000, 1))
	ms := new(big.Int).Quo(stamp.Num(), stamp.Denom())
	if !ms.IsInt64() {
		return nil, errors.New("price: invalid XMR trade time")
	}
	age := now.Sub(time.UnixMilli(ms.Int64()))
	if age > 5*time.Minute || age < -30*time.Second {
		return nil, errors.New("price: XMR feed is stale")
	}
	return value, nil
}
