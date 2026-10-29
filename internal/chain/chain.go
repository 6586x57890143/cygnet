// Package chain is what cygnet reads off public blockchains to see a
// supporter's payment arrive (docs/support.md): the rails it takes, the
// exact amount a checkout asks for on each, and readers that list the
// transfers into a wallet since a cursor. It holds no key and signs
// nothing; every call is a public read.
//
// No web3 library: an ERC-20 transfer is a log with one fixed topic, TRON
// has a REST endpoint for its token transfers, and Solana's RPC returns
// balances already parsed. The address helpers are copied from merlin's
// tip jar (internal/plugins/aimod/erc20.go), as the birds copy shared code
// rather than import it.
package chain

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"strings"
)

// Family is the address form a rail pays into: one 0x address takes every
// EVM chain, a T... address TRON, and a base58 public key Solana.
type Family string

const (
	EVM    Family = "evm"
	Tron   Family = "tron"
	Solana Family = "solana"
)

// Rail is one asset on one chain that a checkout can be paid in.
type Rail struct {
	// Key names it in the database and in a checkout: "base:USDC".
	Key   string
	Chain string
	// Label is how a person reads it: "usdc on base".
	Label  string
	Family Family
	// Decimals is the asset's smallest unit, read off the contract or
	// mint before the rail was added, never assumed.
	Decimals int
	// Token is the ERC-20 or TRC-20 contract, or the SPL mint; "" for SOL.
	Token string
	// RPC is the public endpoint read by default: JSON-RPC for EVM and
	// Solana, TronGrid's REST base for TRON.
	RPC string
	// Confirmations is how many blocks deep an EVM transfer has to be
	// before it counts, so a reorg can't take back a payment cygnet
	// already counted. TRON and Solana have their own word for final.
	Confirmations uint64
	// Stable is a dollar stablecoin, priced one to one; anything else is
	// quoted at the market price when the checkout is made.
	Stable bool
	// Native uses the indexed native-transfer reader, including contract
	// sends. It is offered only with a configured native index endpoint.
	Native  bool
	ChainID uint64
	// Tx is the explorer link for one of its transactions.
	Tx string
}

// Rails is every rail cygnet takes, in the order a checkout offers them:
// the cheapest to send first. Every contract and mint here is the one
// merlin's tip jar checked live against its own symbol() and decimals()
// (Base's is native USDC, not USDbC; Polygon's native USDC, not USDC.e).
//
// ponytail: a fixed table, not any token a supporter holds. Valuing
// arbitrary tokens needs an indexer and a price for each; a supporter
// swaps first, which every wallet does in a tap.
var Rails = []Rail{
	{Key: "solana:USDC", Chain: "solana", Label: "usdc on solana", Family: Solana, Decimals: 6, Stable: true,
		Token: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", RPC: "https://api.mainnet-beta.solana.com", Tx: "https://solscan.io/tx/%s"},
	{Key: "solana:USDT", Chain: "solana", Label: "usdt on solana", Family: Solana, Decimals: 6, Stable: true,
		Token: "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB", RPC: "https://api.mainnet-beta.solana.com", Tx: "https://solscan.io/tx/%s"},
	{Key: "base:USDC", Chain: "base", Label: "usdc on base", Family: EVM, Decimals: 6, Stable: true, Confirmations: 30,
		Token: "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", RPC: "https://base-rpc.publicnode.com", Tx: "https://basescan.org/tx/%s"},
	{Key: "polygon:USDC", Chain: "polygon", Label: "usdc on polygon", Family: EVM, Decimals: 6, Stable: true, Confirmations: 128,
		Token: "0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359", RPC: "https://polygon-bor-rpc.publicnode.com", Tx: "https://polygonscan.com/tx/%s"},
	{Key: "tron:USDT", Chain: "tron", Label: "usdt on tron", Family: Tron, Decimals: 6, Stable: true,
		Token: "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", RPC: "https://api.trongrid.io", Tx: "https://tronscan.org/#/transaction/%s"},
	{Key: "solana:SOL", Chain: "solana", Label: "sol", Family: Solana, Decimals: 9,
		RPC: "https://api.mainnet-beta.solana.com", Tx: "https://solscan.io/tx/%s"},
	{Key: "base:ETH", Chain: "base", Label: "eth on base", Family: EVM, Decimals: 18, Native: true, ChainID: 8453, Confirmations: 1,
		RPC: "https://base-rpc.publicnode.com", Tx: "https://basescan.org/tx/%s"},
	{Key: "ethereum:ETH", Chain: "ethereum", Label: "eth on ethereum", Family: EVM, Decimals: 18, Native: true, ChainID: 1, Confirmations: 1,
		RPC: "https://ethereum-rpc.publicnode.com", Tx: "https://etherscan.io/tx/%s"},
}

// Find is the rail called key.
func Find(key string) (Rail, bool) {
	for _, r := range Rails {
		if r.Key == key {
			return r, true
		}
	}
	return Rail{}, false
}

// Asset is the rail's asset, "USDC", or the chain's own coin, "SOL".
func (r Rail) Asset() string {
	_, a, _ := strings.Cut(r.Key, ":")
	return a
}

// pow10 is 10^n.
func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

// Base is what cents cost on r before any change, in its smallest unit: a
// stablecoin one to one, anything else at usd dollars a coin, rounded up
// to its smallest unit and then to a ten-thousandth of a coin, so the
// change added after it stays the last few digits.
func (r Rail) Base(cents int, usd *big.Rat) (*big.Int, error) {
	if cents <= 0 {
		return nil, errors.New("chain: nothing to price")
	}
	if r.Stable {
		return new(big.Int).Mul(big.NewInt(int64(cents)), pow10(r.Decimals-2)), nil
	}
	if usd == nil || usd.Sign() <= 0 {
		return nil, fmt.Errorf("chain: no price for %s", r.Key)
	}
	// cents / 100 / usd coins, in units: cents * 10^decimals / (100 * usd).
	units := new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(int64(cents)), pow10(r.Decimals)), big.NewInt(100))
	units.Quo(units, usd)
	out := ceil(units)
	step := pow10(r.Decimals - 4)
	if m := new(big.Int).Mod(out, step); m.Sign() != 0 {
		out.Add(out, new(big.Int).Sub(step, m))
	}
	return out, nil
}

func ceil(x *big.Rat) *big.Int {
	q, m := new(big.Int).QuoRem(x.Num(), x.Denom(), new(big.Int))
	if m.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return q
}

// MaxChange is the most change a checkout on r adds to its price: a cent's
// worth less one unit on a stablecoin (9999 units, under $0.01), a
// ten-thousandth of a coin less one unit otherwise.
func (r Rail) MaxChange() *big.Int {
	if r.Stable {
		return new(big.Int).Sub(pow10(r.Decimals-2), big.NewInt(1))
	}
	return new(big.Int).Sub(pow10(r.Decimals-4), big.NewInt(1))
}

// Pick is an amount for a checkout of cents on r: its Base plus change
// drawn at random from 1 to MaxChange, so two checkouts at once almost
// never ask for the same thing, and the store refuses the rare one that
// does.
func (r Rail) Pick(cents int, usd *big.Rat) (*big.Int, error) {
	base, err := r.Base(cents, usd)
	if err != nil {
		return nil, err
	}
	change, err := rand.Int(rand.Reader, r.MaxChange())
	if err != nil {
		return nil, err
	}
	return base.Add(base, change.Add(change, big.NewInt(1))), nil
}

// Format is amount in whole coins, every digit it has: "5.004217". Never
// rounded, since the digits are what say which checkout it pays.
func (r Rail) Format(amount *big.Int) string {
	s := amount.String()
	if len(s) <= r.Decimals {
		s = strings.Repeat("0", r.Decimals-len(s)+1) + s
	}
	whole, frac := s[:len(s)-r.Decimals], strings.TrimRight(s[len(s)-r.Decimals:], "0")
	if frac == "" {
		return whole
	}
	return whole + "." + frac
}

// Pay is a link a wallet opens with the payment filled in, where the rail
// has a standard one: Solana Pay's transfer request on Solana (its spec:
// the recipient is the wallet, never a token account, and the amount is in
// whole coins). "" elsewhere, where the address and amount are copied.
func (r Rail) Pay(to string, amount *big.Int, label, message string) string {
	if r.Family != Solana {
		return ""
	}
	q := url.Values{}
	q.Set("amount", r.Format(amount))
	if r.Token != "" {
		q.Set("spl-token", r.Token)
	}
	q.Set("label", label)
	q.Set("message", message)
	// url.Values encodes a space as +, which Solana Pay reads literally:
	// its spec wants percent encoding.
	return "solana:" + to + "?" + strings.ReplaceAll(q.Encode(), "+", "%20")
}

// Valid is whether address is well formed for family. It says nothing
// about whether anybody holds the key to it: a TRON address carries a
// checksum, an EVM one isn't checksummed here (all lowercase is valid) and
// a Solana one has none to check.
func Valid(family Family, address string) bool {
	switch family {
	case EVM:
		return evmAddress.MatchString(address)
	case Tron:
		_, err := tronHex(address)
		return err == nil
	case Solana:
		return solanaKey(address) == nil
	}
	return false
}

var (
	evmAddress  = regexp.MustCompile("^0x[0-9a-fA-F]{40}$")
	tronAddress = regexp.MustCompile("^T[1-9A-HJ-NP-Za-km-z]{33}$")
	solanaShape = regexp.MustCompile("^[1-9A-HJ-NP-Za-km-z]{32,44}$")
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58Decode is a base58 string's bytes, its leading '1's as leading
// zero bytes.
func base58Decode(s string) ([]byte, error) {
	num := new(big.Int)
	radix := big.NewInt(58)
	for _, c := range s {
		i := strings.IndexRune(base58Alphabet, c)
		if i < 0 {
			return nil, fmt.Errorf("%q is not base58", s)
		}
		num.Mul(num, radix)
		num.Add(num, big.NewInt(int64(i)))
	}
	out := num.Bytes()
	for i := 0; i < len(s) && s[i] == '1'; i++ {
		out = append([]byte{0}, out...)
	}
	return out, nil
}

// tronHex is a TRON address's 20 bytes in hex, its checksum checked.
func tronHex(address string) (string, error) {
	if !tronAddress.MatchString(address) {
		return "", fmt.Errorf("tron: %q is not an address", address)
	}
	b, err := base58Decode(address)
	if err != nil || len(b) != 25 {
		return "", fmt.Errorf("tron: %q is not an address", address)
	}
	payload, sum := b[:21], b[21:]
	first := sha256.Sum256(payload)
	second := sha256.Sum256(first[:])
	if !bytes.Equal(second[:4], sum) || payload[0] != 0x41 {
		return "", fmt.Errorf("tron: %q has a bad checksum or isn't mainnet", address)
	}
	return hex.EncodeToString(payload[1:]), nil
}

// solanaKey checks address decodes to a 32 byte public key.
func solanaKey(address string) error {
	if !solanaShape.MatchString(address) {
		return fmt.Errorf("solana: %q is not a key", address)
	}
	b, err := base58Decode(address)
	if err != nil || len(b) != 32 {
		return fmt.Errorf("solana: %q is not a key", address)
	}
	return nil
}
