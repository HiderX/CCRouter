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
	return &Reader{Store: s, Client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, lastRun: map[string]time.Time{}, lastError: map[string]string{}}
}

// Exit is the probed egress address. Country is a best-effort ISO 3166-1 code.
type Exit struct {
	IP      string `json:"ip"`
	Country string `json:"country,omitempty"`
}

func (r *Reader) IP(ctx context.Context, force bool) (Exit, time.Time, error) {
	if b, t, e := r.Store.Snapshot("ip"); e == nil && !force && time.Since(t) < 15*time.Minute {
		var x Exit
		// Snapshots written before country lookup hold the bare address.
		if json.Unmarshal(b, &x) != nil {
			x = Exit{IP: string(b)}
		}
		if net.ParseIP(x.IP) != nil {
			return x, t, nil
		}
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org?format=json", nil)
	res, e := r.Client.Do(req)
	if e != nil {
		return Exit{}, time.Time{}, errors.New("出口 IP 暂时无法查询")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return Exit{}, time.Time{}, fmt.Errorf("IP 查询 HTTP %d", res.StatusCode)
	}
	var v struct {
		IP string `json:"ip"`
	}
	if e = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&v); e != nil || net.ParseIP(v.IP) == nil {
		return Exit{}, time.Time{}, errors.New("IP 查询返回无效地址")
	}
	x := Exit{IP: v.IP, Country: r.country(ctx, v.IP)}
	b, _ := json.Marshal(x)
	if e = r.Store.SaveSnapshot("ip", b); e != nil {
		return Exit{}, time.Time{}, e
	}
	return x, time.Now(), nil
}

// country looks up the probed address itself, so the result describes that IP even if
// this request takes a different route. Failure only hides the country.
func (r *Reader) country(ctx context.Context, ip string) string {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://ipinfo.io/"+ip+"/country", nil)
	if e != nil {
		return ""
	}
	res, e := r.Client.Do(req)
	if e != nil {
		return ""
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 16))
	code := strings.TrimSpace(string(b))
	if res.StatusCode != 200 || len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return ""
	}
	return code
}
