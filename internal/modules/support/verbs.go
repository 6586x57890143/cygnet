package support

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/disgoorg/snowflake/v2"

	"github.com/6586x57890143/cygnet/internal/chain"
	"github.com/6586x57890143/cygnet/internal/store"
	"github.com/6586x57890143/cygnet/link"
)

// What the hub answers about supporters (docs/support.md). cygnet.lol's
// Worker, as the bird edge, asks what is on offer, opens a checkout for
// whoever signed in and watches it; any linked bird asks who backs the
// birds, to give them its supporter role. The Worker says who signed in,
// and only the Worker is trusted to.

// edgeBird is cygnet.lol's Worker: the only caller that opens or reads a
// checkout.
const edgeBird = "edge"

// Why a verb is refused, as a denied error's reason, for the Worker to say
// in its own words.
const (
	NoRails    = "no_rails"
	NotOffered = "not_offered"
	TooMany    = "too_many"
	NoCheckout = "no_checkout"
	NotYours   = "not_yours"
)

// Life is how long a checkout's amount holds: an hour on a stablecoin, 20
// minutes where the price moves.
const (
	stableLife   = time.Hour
	volatileLife = 20 * time.Minute
)

// Ask is a verb's body: who signed in and, as the verb needs, what they
// want or which checkout.
type Ask struct {
	User   string `json:"user"`
	Months int    `json:"months,omitempty"`
	Cents  int    `json:"cents,omitempty"`
	Rail   string `json:"rail,omitempty"`
	ID     string `json:"id,omitempty"`
}

// Offer is cygnet.offer's answer: what can be bought, on which rails, and
// who it goes to.
type Offer struct {
	Plans []OfferPlan `json:"plans"`
	// Rates is the monthly amounts, in US cents, the first the plain one.
	Rates []int       `json:"rates"`
	Rails []OfferRail `json:"rails"`
	Owner Owner       `json:"owner"`
}

// OfferPlan is one length and its price at each rate, by rate.
type OfferPlan struct {
	Months int         `json:"months"`
	Prices map[int]int `json:"prices"`
}

// OfferRail is a rail as a checkout offers it.
type OfferRail struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Owner is who the money goes to, as their page names them.
type Owner struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Avatar string `json:"avatar,omitempty"`
}

// Checkout is a checkout as its page shows it. Times are Unix
// milliseconds, 0 for not yet.
type Checkout struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Months int    `json:"months"`
	Cents  int    `json:"cents"`
	Price  int    `json:"price"`
	Gold   bool   `json:"gold"`
	Rail   string `json:"rail"`
	Label  string `json:"label"`
	// Amount is what to send, in whole coins with every digit that counts;
	// Units the same in the rail's smallest unit.
	Amount string `json:"amount"`
	Units  string `json:"units"`
	// To is the wallet it goes to, and Pay a link a wallet opens with all
	// of it filled in, where the rail has one.
	To      string `json:"to"`
	Pay     string `json:"pay,omitempty"`
	Quote   string `json:"quote,omitempty"`
	Expires int64  `json:"expires"`
	Paid    int64  `json:"paid,omitempty"`
	Until   int64  `json:"until,omitempty"`
	// Stale is a quoted checkout past its time and VolatileSlack: still
	// lapsed, but nothing sent now pays it.
	Stale bool `json:"stale,omitempty"`
}

// Status is someone's backing, for their page and for the birds. Times are
// Unix milliseconds.
type Status struct {
	Active bool  `json:"active"`
	Gold   bool  `json:"gold"`
	Since  int64 `json:"since,omitempty"`
	Until  int64 `json:"until,omitempty"`
}

// Backer is one person backing the birds now, for a bird's role.
type Backer struct {
	User  string `json:"user"`
	Gold  bool   `json:"gold"`
	Until int64  `json:"until"`
}

// Verbs is every verb this module answers, by name, for main to serve on
// the hub.
func (m *Module) Verbs() map[string]func(context.Context, string, json.RawMessage) (any, error) {
	return map[string]func(context.Context, string, json.RawMessage) (any, error){
		"offer": m.offer, "quote": m.quote, "checkout": m.checkout, "supporter": m.supporter, "supporters": m.supporters,
	}
}

// read decodes a call's body and who it is about. A verb only the Worker
// may ask refuses any other caller.
func (m *Module) read(caller string, body json.RawMessage, edgeOnly bool) (Ask, snowflake.ID, error) {
	var a Ask
	if edgeOnly && caller != edgeBird {
		return a, 0, link.Err(link.Denied, "only cygnet.lol opens checkouts")
	}
	if m.store == nil {
		return a, 0, link.Err(link.Denied, "cygnet keeps no supporters without a database")
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &a); err != nil {
			return a, 0, link.Err(link.Invalid, "body")
		}
	}
	if a.User == "" {
		return a, 0, nil
	}
	user, err := snowflake.Parse(a.User)
	if err != nil || user == 0 {
		return a, 0, link.Err(link.Invalid, "user is a Discord ID")
	}
	return a, user, nil
}

func (m *Module) offer(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	if _, _, err := m.read(caller, body, true); err != nil {
		return nil, err
	}
	rails := m.rails()
	if len(rails) == 0 {
		return nil, link.Err(link.Denied, NoRails)
	}
	o, err := m.store.Offer(ctx)
	if err != nil {
		return nil, err
	}
	out := Offer{Rates: o.Rates, Owner: m.ownerProfile(ctx)}
	for _, p := range o.Plans {
		op := OfferPlan{Months: p.Months, Prices: map[int]int{}}
		for _, c := range o.Rates {
			op.Prices[c] = p.Price(c)
		}
		out.Plans = append(out.Plans, op)
	}
	for _, r := range rails {
		out.Rails = append(out.Rails, OfferRail{Key: r.Key, Label: r.Label})
	}
	return out, nil
}

func (m *Module) quote(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	a, user, err := m.read(caller, body, true)
	if err != nil {
		return nil, err
	}
	if user == 0 {
		return nil, link.Err(link.Invalid, "a checkout is for someone")
	}
	rail, ok := m.rail(a.Rail)
	if !ok {
		return nil, link.Err(link.Denied, NotOffered)
	}
	life := stableLife
	if !rail.Stable {
		life = volatileLife
	}
	ask := store.Ask{User: user, Months: a.Months, Cents: a.Cents, Rail: rail.Key}
	// A price is read only for a new checkout, and before the store's
	// transaction, never inside it: one already open comes back without
	// one, price feed down or not, and a slow feed holds no connection.
	var usd *big.Rat
	quote := ""
	if !rail.Stable {
		if same, err := m.store.Reusable(ctx, ask, m.now(), life); err == nil {
			return m.view(same), nil
		} else if !errors.Is(err, store.ErrNoInvoice) {
			return nil, err
		}
		if usd, err = m.price(ctx, rail.Asset()); err != nil {
			m.log.Warn("quoting a checkout: no price", "rail", rail.Key, "err", err)
			return nil, link.Err(link.Busy, "no price for "+rail.Asset()+" right now")
		}
		quote = usd.FloatString(2)
	}
	i, err := m.store.Quote(ctx, ask, m.now(), life, func(price int) (*big.Int, string, error) {
		amount, err := rail.Pick(price, usd)
		return amount, quote, err
	})
	switch {
	case errors.Is(err, store.ErrNotOffered):
		return nil, link.Err(link.Denied, NotOffered)
	case errors.Is(err, store.ErrTooMany):
		return nil, link.Err(link.Denied, TooMany)
	case errors.Is(err, store.ErrNoAmount):
		return nil, link.Err(link.Busy, "every amount on that rail is taken right now")
	case err != nil:
		return nil, err
	}
	return m.view(i), nil
}

func (m *Module) checkout(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	a, user, err := m.read(caller, body, true)
	if err != nil {
		return nil, err
	}
	// An id is 26 base32 characters: anything else is no checkout, without
	// asking the database.
	if len(a.ID) != 26 || strings.Trim(a.ID, "abcdefghijklmnopqrstuvwxyz234567") != "" {
		return nil, link.Err(link.Denied, NoCheckout)
	}
	i, err := m.store.Invoice(ctx, a.ID)
	switch {
	case errors.Is(err, store.ErrNoInvoice):
		return nil, link.Err(link.Denied, NoCheckout)
	case err != nil:
		return nil, err
	case i.User != user:
		// Someone else's checkout says so: its id is 128 random bits, so
		// telling it apart from none tells a guesser nothing.
		return nil, link.Err(link.Denied, NotYours)
	}
	return m.view(i), nil
}

// view is a checkout as its page shows it, with the wallet it pays and
// the link that pays it.
func (m *Module) view(i store.Invoice) Checkout {
	rail, _ := chain.Find(i.Rail)
	to := m.wallets.of(rail.Family)
	c := Checkout{
		ID: i.ID, State: i.State, Months: i.Months, Cents: i.Cents, Price: i.Price, Gold: i.Gold,
		Rail: i.Rail, Label: rail.Label, Amount: rail.Format(i.Amount), Units: i.Amount.String(),
		To: to, Quote: i.Quote, Expires: i.Expires.UnixMilli(),
	}
	// ponytail: the wallet is read from the environment, not kept on the
	// checkout, so change one only with none open on it (the panel lists
	// them); a checkout whose wallet is unset offers nowhere to pay.
	c.Stale = i.Quote != "" && i.State == store.InvoiceLapsed && !m.now().Before(i.Expires.Add(store.VolatileSlack))
	if to != "" && !c.Stale && (i.State == store.InvoiceOpen || i.State == store.InvoiceLapsed) {
		c.Pay = rail.Pay(to, i.Amount, "back the birds", months(i.Months)+" of backing")
	}
	if !i.Paid.IsZero() {
		c.Paid = i.Paid.UnixMilli()
	}
	if !i.PeriodEnds.IsZero() {
		c.Until = i.PeriodEnds.UnixMilli()
	}
	return c
}

func (m *Module) supporter(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	_, user, err := m.read(caller, body, false)
	if err != nil {
		return nil, err
	}
	if user == 0 {
		return nil, link.Err(link.Invalid, "user is a Discord ID")
	}
	st, err := m.store.Status(ctx, user, m.now())
	if err != nil {
		return nil, err
	}
	return status(st), nil
}

func status(st store.Status) Status {
	out := Status{Active: st.Active, Gold: st.Gold}
	if !st.Since.IsZero() {
		out.Since, out.Until = st.Since.UnixMilli(), st.Until.UnixMilli()
	}
	return out
}

func (m *Module) supporters(ctx context.Context, caller string, body json.RawMessage) (any, error) {
	if _, _, err := m.read(caller, body, false); err != nil {
		return nil, err
	}
	all, err := m.store.Supporters(ctx, m.now())
	if err != nil {
		return nil, err
	}
	out := make([]Backer, 0, len(all))
	for _, s := range all {
		out = append(out, Backer{User: s.User.String(), Gold: s.Gold, Until: s.Until.UnixMilli()})
	}
	return map[string][]Backer{"supporters": out}, nil
}

// priceFor is how long a coin's price is reused across quotes: every SOL
// quote in it reads the feed once, so a busy page can't hammer Coinbase.
const priceFor = 30 * time.Second

// price is asset's spot price, read at most every priceFor.
func (m *Module) price(ctx context.Context, asset string) (*big.Rat, error) {
	m.prices.Lock()
	defer m.prices.Unlock()
	if p, ok := m.prices.got[asset]; ok && m.now().Sub(p.at) < p.life {
		return p.usd, p.err
	}
	usd, err := m.reader.USD(ctx, asset)
	if m.prices.got == nil {
		m.prices.got = map[string]spot{}
	}
	// A failed read is remembered briefly too, so quotes queued behind it
	// during an outage don't each wait out their own.
	life := priceFor
	if err != nil {
		life = priceFor / 3
	}
	m.prices.got[asset] = spot{usd, err, m.now(), life}
	return usd, err
}

// spot is a price (or why there was none), when it was read and how long
// it is good for.
type spot struct {
	usd  *big.Rat
	err  error
	at   time.Time
	life time.Duration
}
