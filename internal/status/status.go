// Package status reads account quota through Claude Code and probes the exit IP.
// It never proxies Claude requests or handles credentials.
package status

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"ccrouter/internal/store"
)

type Window struct {
	Utilization float64    `json:"utilization"`
	ResetsAt    *time.Time `json:"resets_at"`
}
type Quota struct {
	FiveHour *Window `json:"five_hour"`
	SevenDay *Window `json:"seven_day"`
}
type Result struct {
	Quota   Quota
	Updated time.Time
	Error   string
}
type Reader struct {
	Store     *store.Store
	Client    *http.Client // exit IP probes only
	mu        sync.Mutex
	lastRun   map[string]time.Time
	lastError map[string]string
}

func New(s *store.Store) *Reader {
	// A kept connection stays on the route it opened on and would hide a change of proxy node.
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	return &Reader{Store: s, Client: &http.Client{Transport: t, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, lastRun: map[string]time.Time{}, lastError: map[string]string{}}
}

const (
	// traceURL is Cloudflare's diagnostics page on the host Claude Code talks to, so a
	// proxy routes it as it routes Claude. Cloudflare answers it itself. It is the one
	// request CCRouter sends to an Anthropic domain and carries nothing about any account.
	traceURL = "https://api.anthropic.com/cdn-cgi/trace"
	ipFresh  = time.Minute // a probe this young serves the panel as it opens; a refresh always probes
)

// Exit is the probed egress address. Country is a best-effort ISO 3166-1 code.
type Exit struct {
	IP      string `json:"ip"`
	Country string `json:"country,omitempty"`
}

func (r *Reader) IP(ctx context.Context, force bool) (Exit, time.Time, error) {
	if b, t, e := r.Store.Snapshot("ip"); e == nil && !force && time.Since(t) < ipFresh {
		var x Exit
		// Snapshots written before country lookup hold the bare address.
		if json.Unmarshal(b, &x) != nil {
			x = Exit{IP: string(b)}
		}
		if net.ParseIP(x.IP) != nil {
			return x, t, nil
		}
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, traceURL, nil)
	res, e := r.Client.Do(req)
	if e != nil {
		return Exit{}, time.Time{}, errors.New("出口 IP 暂时无法查询")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return Exit{}, time.Time{}, fmt.Errorf("IP 查询 HTTP %d", res.StatusCode)
	}
	body, e := io.ReadAll(io.LimitReader(res.Body, 4096))
	if e != nil {
		return Exit{}, time.Time{}, errors.New("出口 IP 暂时无法查询")
	}
	x := trace(string(body))
	if net.ParseIP(x.IP) == nil {
		return Exit{}, time.Time{}, errors.New("IP 查询返回无效地址")
	}
	b, _ := json.Marshal(x)
	if e = r.Store.SaveSnapshot("ip", b); e != nil {
		return Exit{}, time.Time{}, e
	}
	return x, time.Now(), nil
}

// trace reads the address and country from Cloudflare's key=value lines. A country that
// is not a plain two-letter code, such as T1 for Tor, is left out.
func trace(body string) Exit {
	var x Exit
	for _, line := range strings.Split(body, "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch {
		case k == "ip":
			x.IP = v
		case k == "loc" && len(v) == 2 && v[0] >= 'A' && v[0] <= 'Z' && v[1] >= 'A' && v[1] <= 'Z':
			x.Country = v
		}
	}
	return x
}
