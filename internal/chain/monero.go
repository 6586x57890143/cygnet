package chain

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/icholy/digest"
	"golang.org/x/crypto/sha3"
)

// Monero is a private view-only wallet service. Its spend key and view key
// are never supplied to this client. It exposes no spend or key-export API.
type Monero struct {
	Primary       string
	Network       string
	endpoint      string
	client        *http.Client
	checked       atomic.Int64
	authenticated atomic.Bool
}

// MoneroAddress checks Monero's block-base58 encoding and Keccak checksum.
// Only ordinary addresses and subaddresses on Mainnet/Stagenet are accepted.
func MoneroAddress(address string) (network string, subaddress bool, ok bool) {
	if len(address) != 95 {
		return "", false, false
	}
	raw := make([]byte, 0, 69)
	for offset := 0; offset < len(address); offset += 11 {
		end := min(offset+11, len(address))
		n := 8
		if end-offset == 7 {
			n = 5
		}
		block, err := base58Decode(strings.TrimLeft(address[offset:end], "1"))
		if err != nil || len(block) > n {
			return "", false, false
		}
		raw = append(raw, make([]byte, n-len(block))...)
		raw = append(raw, block...)
	}
	if len(raw) != 69 {
		return "", false, false
	}
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write(raw[:65])
	if !bytes.Equal(h.Sum(nil)[:4], raw[65:]) {
		return "", false, false
	}
	switch raw[0] {
	case 18:
		return "Mainnet", false, true
	case 42:
		return "Mainnet", true, true
	case 24:
		return "Stagenet", false, true
	case 36:
		return "Stagenet", true, true
	}
	return "", false, false
}

// NewMonero accepts only loopback, private IPs or the dedicated container
// name. Redirects and environment HTTP proxies cannot forward credentials.
// Legacy Digest is accepted only over certificate-verified TLS with a long
// password. Optional PEM roots pin a private wallet service's trust anchor.
func NewMonero(endpoint, primary, network, user, password string, certificates ...[]byte) (*Monero, error) {
	u, err := url.Parse(endpoint)
	if network == "" {
		network = "Mainnet"
	}
	actual, sub, valid := MoneroAddress(primary)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || user == "" || password == "" || !valid || sub || actual != network {
		return nil, errors.New("monero: invalid private wallet configuration")
	}
	host := u.Hostname()
	ip, _ := netip.ParseAddr(host)
	if host != "localhost" && host != "xmr-wallet" && !ip.IsLoopback() && !ip.IsPrivate() {
		return nil, errors.New("monero: wallet RPC must stay on the private network")
	}
	if u.Scheme == "http" && host != "localhost" && !ip.IsLoopback() {
		return nil, errors.New("monero: non-loopback wallet RPC requires TLS")
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/json_rpc"
	}
	if u.Path != "/json_rpc" {
		return nil, errors.New("monero: invalid RPC path")
	}
	secure := &tls.Config{MinVersion: tls.VersionTLS12}
	if len(certificates) > 1 || (len(certificates) == 1 && u.Scheme != "https") {
		return nil, errors.New("monero: invalid private TLS configuration")
	}
	if len(certificates) == 1 {
		secure.RootCAs = x509.NewCertPool()
		if !secure.RootCAs.AppendCertsFromPEM(certificates[0]) {
			return nil, errors.New("monero: invalid private TLS certificate")
		}
	}
	base := &http.Transport{Proxy: nil, TLSClientConfig: secure, MaxIdleConnsPerHost: 2, IdleConnTimeout: time.Minute, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("monero: invalid private peer")
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, errors.New("monero: private peer unavailable")
		}
		for _, ip := range ips {
			ip = ip.Unmap()
			if !ip.IsPrivate() && !ip.IsLoopback() {
				return nil, errors.New("monero: refusing public peer")
			}
		}
		var last error
		for _, ip := range ips {
			conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		if last == nil {
			last = errors.New("monero: no private peer")
		}
		return nil, last
	}}
	m := &Monero{Primary: primary, Network: network, endpoint: u.String()}
	transport := &digest.Transport{Username: user, Password: password, Transport: base, Digest: func(req *http.Request, challenge *digest.Challenge, options digest.Options) (*digest.Credentials, error) {
		switch strings.ToUpper(challenge.Algorithm) {
		case "SHA-256":
		case "MD5", "MD5-SESS":
			if req.URL.Scheme != "https" || len(password) < 32 {
				return nil, errors.New("monero: legacy authentication requires verified TLS and a long password")
			}
		default:
			return nil, errors.New("monero: unsupported wallet authentication")
		}
		credentials, err := digest.Digest(challenge, options)
		if err == nil {
			m.authenticated.Store(true)
		}
		return credentials, err
	}}
	m.client = &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return m, nil
}

func (m *Monero) rpc(ctx context.Context, method string, params, out any) error {
	switch method {
	case "get_address", "get_height", "refresh", "create_address", "store", "get_transfers":
	default:
		return errors.New("monero: method not allowed")
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return errors.New("monero: encoding request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("monero: constructing request")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := m.client.Do(req)
	if err != nil {
		return errors.New("monero: private wallet unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("monero: wallet answered %d", response.StatusCode)
	}
	if !m.authenticated.Load() {
		return errors.New("monero: wallet RPC authentication is disabled")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20+1))
	if err != nil || len(raw) > 2<<20 {
		return errors.New("monero: oversized or unreadable response")
	}
	var reply struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil || reply.ID != 1 || reply.Error != nil || len(reply.Result) == 0 || string(reply.Result) == "null" {
		return errors.New("monero: wallet RPC refused the operation")
	}
	if out != nil && json.Unmarshal(reply.Result, out) != nil {
		return errors.New("monero: invalid wallet response")
	}
	return nil
}

// Ready is short-lived: a stopped or unsynchronized service cannot keep
// issuing quotes from an earlier healthy read.
func (m *Monero) Ready(now time.Time) bool {
	checked := m.checked.Load()
	return checked != 0 && now.UnixMilli() >= checked && now.Sub(time.UnixMilli(checked)) < 2*time.Minute
}

func (m *Monero) Invalidate() { m.checked.Store(0) }

func (m *Monero) Check(ctx context.Context, now time.Time) error {
	m.checked.Store(0)
	var info struct {
		Address string `json:"address"`
	}
	if err := m.rpc(ctx, "get_address", map[string]any{"account_index": 0}, &info); err != nil {
		return err
	}
	if info.Address != m.Primary {
		return errors.New("monero: wallet does not match its pinned address")
	}
	// Released wallet RPC has no wallet-type/daemon-height metadata call.
	// The image's offline startup guard verifies type and network before
	// RPC starts. A successful refresh reaches the configured daemon's tip;
	// an unavailable daemon or an unfinished refresh never marks it ready.
	if err := m.rpc(ctx, "refresh", map[string]any{}, &struct{}{}); err != nil {
		return err
	}
	var head struct {
		Height uint64 `json:"height"`
	}
	if err := m.rpc(ctx, "get_height", map[string]any{}, &head); err != nil {
		return err
	}
	if head.Height == 0 {
		return errors.New("monero: pinned wallet is not synchronized")
	}
	m.checked.Store(now.UnixMilli())
	return nil
}

type Subaddress struct {
	Address string
	Index   uint32
}

func (m *Monero) CreateAddresses(ctx context.Context) ([]Subaddress, error) {
	var result struct {
		Addresses []string `json:"addresses"`
		Indices   []uint32 `json:"address_indices"`
	}
	if err := m.rpc(ctx, "create_address", map[string]any{"account_index": 0, "count": 8}, &result); err != nil {
		return nil, err
	}
	if len(result.Addresses) != 8 || len(result.Indices) != 8 {
		return nil, errors.New("monero: incomplete address batch")
	}
	out := make([]Subaddress, 0, 8)
	seen := map[uint32]bool{}
	for n, address := range result.Addresses {
		network, sub, valid := MoneroAddress(address)
		index := result.Indices[n]
		if !valid || !sub || network != m.Network || index == 0 || seen[index] {
			return nil, errors.New("monero: invalid subaddress batch")
		}
		seen[index] = true
		out = append(out, Subaddress{Address: address, Index: index})
	}
	// Persist the wallet's allocation before exposing addresses to Postgres.
	if err := m.rpc(ctx, "store", map[string]any{}, nil); err != nil {
		return nil, err
	}
	return out, nil
}

// Incoming re-reads confirmed receipts for a bounded group of subaddresses.
// Locked/unconfirmed receipts are reconsidered next round. Amount is the
// transaction total into one subaddress; its identity includes that index.
func (m *Monero) Incoming(ctx context.Context, addresses map[uint32]string) ([]Transfer, error) {
	indices := make([]uint32, 0, len(addresses))
	for index := range addresses {
		indices = append(indices, index)
	}
	if len(indices) == 0 {
		return nil, nil
	}
	if len(indices) > 32 {
		return nil, errors.New("monero: too many watched addresses")
	}
	var result struct {
		In []struct {
			Tx            string  `json:"txid"`
			Amount        uint64  `json:"amount"`
			Confirmations uint64  `json:"confirmations"`
			Height        uint64  `json:"height"`
			Timestamp     int64   `json:"timestamp"`
			Locked        *bool   `json:"locked"`
			Double        bool    `json:"double_spend_seen"`
			Unlock        *uint64 `json:"unlock_time"`
			Index         struct {
				Major uint32 `json:"major"`
				Minor uint32 `json:"minor"`
			} `json:"subaddr_index"`
		} `json:"in"`
	}
	if err := m.rpc(ctx, "get_transfers", map[string]any{"in": true, "account_index": 0, "subaddr_indices": indices}, &result); err != nil {
		return nil, err
	}
	if len(result.In) > 1000 {
		return nil, errors.New("monero: transfer batch is too large")
	}
	var out []Transfer
	seen := map[string]bool{}
	for _, t := range result.In {
		to, ok := addresses[t.Index.Minor]
		if !ok || t.Index.Major != 0 || len(t.Tx) != 64 || !hash32.MatchString("0x"+t.Tx) || t.Amount == 0 || t.Timestamp <= 0 {
			return nil, errors.New("monero: invalid incoming receipt")
		}
		if t.Height == 0 || t.Confirmations < 10 || t.Locked == nil || *t.Locked || t.Unlock == nil || *t.Unlock != 0 || t.Double {
			continue
		}
		event := fmt.Sprintf("subaddress:%d", t.Index.Minor)
		identity := t.Tx + ":" + event
		if seen[identity] {
			return nil, errors.New("monero: duplicate receipt aggregate")
		}
		seen[identity] = true
		out = append(out, Transfer{Tx: strings.ToLower(t.Tx), Event: event, To: to, Amount: new(big.Int).SetUint64(t.Amount), At: time.Unix(t.Timestamp, 0)})
	}
	return out, nil
}
