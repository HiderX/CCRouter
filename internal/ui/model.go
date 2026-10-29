package ui

import (
	"ccrouter/internal/store"
	"fmt"
	"strings"
	"time"
)

// The trend covers the last hour.
const trendBuckets = 40

type snapshot struct {
	Accounts  []store.Account
	Total     store.Totals
	ByAccount map[string]store.Totals
	Trend     []store.Totals
	Recent    []store.Session
}

func load(s *store.Store, period int) (snapshot, error) {
	var v snapshot
	var e error
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if period == 1 {
		start = start.AddDate(0, 0, -6)
	}
	if period == 2 {
		start = time.Unix(0, 0)
	}
	v.Accounts, e = s.Accounts()
	if e != nil {
		return v, e
	}
	v.Total, e = s.Totals("", start, now.Add(time.Second))
	if e != nil {
		return v, e
	}
	v.ByAccount = map[string]store.Totals{}
	for _, a := range v.Accounts {
		t, e := s.Totals(a.ID, start, now.Add(time.Second))
		if e != nil {
			return v, e
		}
		v.ByAccount[a.ID] = t
	}
	v.Trend, e = s.Trend(now.Add(-time.Hour), trendBuckets, time.Hour/trendBuckets)
	if e != nil {
		return v, e
	}
	// Extra rows cover sessions the menu skips because nothing was said in them.
	v.Recent, e = s.Recent("", 40)
	return v, e
}

// compact keeps three significant digits: 2.40M, 240K, 1.2K.
func compact(n int64) string {
	switch {
	case n >= 999_500_000:
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	case n >= 999_500:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	case n >= 9_950:
		return fmt.Sprintf("%.0fK", float64(n)/1e3)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

// regions covers common egress locations. A full CLDR table would add 2.5 MB to the
// binary; any other code is shown as is.
var regions = map[string]string{
	"AE": "阿联酋", "AR": "阿根廷", "AT": "奥地利", "AU": "澳大利亚", "BE": "比利时", "BR": "巴西",
	"CA": "加拿大", "CH": "瑞士", "CL": "智利", "CN": "中国", "CZ": "捷克", "DE": "德国",
	"DK": "丹麦", "ES": "西班牙", "FI": "芬兰", "FR": "法国", "GB": "英国", "HK": "香港",
	"HU": "匈牙利", "ID": "印度尼西亚", "IE": "爱尔兰", "IL": "以色列", "IN": "印度", "IT": "意大利",
	"JP": "日本", "KR": "韩国", "KZ": "哈萨克斯坦", "LU": "卢森堡", "MO": "澳门", "MX": "墨西哥",
	"MY": "马来西亚", "NL": "荷兰", "NO": "挪威", "NZ": "新西兰", "PH": "菲律宾", "PL": "波兰",
	"PT": "葡萄牙", "RO": "罗马尼亚", "RU": "俄罗斯", "SA": "沙特阿拉伯", "SE": "瑞典", "SG": "新加坡",
	"TH": "泰国", "TR": "土耳其", "TW": "台湾", "UA": "乌克兰", "US": "美国", "VN": "越南", "ZA": "南非",
}

// region names an ISO 3166-1 code in Simplified Chinese.
func region(code string) string {
	if n, ok := regions[code]; ok {
		return n
	}
	if code == "ZZ" {
		return ""
	}
	return code
}

// planLabel turns Claude's subscriptionType (max, pro, team…) into a badge title.
func planLabel(p string) string {
	if p == "" {
		return ""
	}
	return strings.ToUpper(p[:1]) + p[1:]
}
