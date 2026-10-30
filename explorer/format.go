package main

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

var displayTZ = time.Local

// formatUnits formata um inteiro em unidades minimas com separadores pt-BR: 1.234.567,89
func formatUnits(v *big.Int, decimals int) string {
	if v == nil {
		return "0"
	}
	neg := v.Sign() < 0
	a := new(big.Int).Abs(v)
	s := a.String()
	var intPart, frac string
	if decimals > 0 {
		if len(s) <= decimals {
			s = strings.Repeat("0", decimals-len(s)+1) + s
		}
		intPart, frac = s[:len(s)-decimals], strings.TrimRight(s[len(s)-decimals:], "0")
	} else {
		intPart = s
	}
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	out := b.String()
	if decimals > 0 {
		if len(frac) < 2 {
			frac += strings.Repeat("0", 2-len(frac))
		}
		out += "," + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

func fmtInt(n interface{}) string {
	s := fmt.Sprint(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func fmtTime(ts uint64) string {
	if ts == 0 {
		return "-"
	}
	return time.Unix(int64(ts), 0).In(displayTZ).Format("02/01/2006 15:04:05")
}

func ago(ts uint64) string {
	if ts == 0 {
		return ""
	}
	d := time.Since(time.Unix(int64(ts), 0))
	switch {
	case d < 0:
		return "agora"
	case d < time.Minute:
		return fmt.Sprintf("há %ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("há %d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("há %d h", int(d.Hours()))
	default:
		return fmt.Sprintf("há %d dias", int(d.Hours()/24))
	}
}

func fmtDuration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	d = d.Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func shortHex(s string) string {
	if len(s) <= 14 {
		return s
	}
	return s[:8] + "…" + s[len(s)-6:]
}

func hexBytes(b []byte) string { return "0x" + hex.EncodeToString(b) }

type pager struct {
	Page, Pages, Total, Size int
	Base                     string // url com "?" ou "&" ja no final
}

func (p pager) HasPrev() bool { return p.Page > 1 }
func (p pager) HasNext() bool { return p.Page < p.Pages }
func (p pager) Prev() int     { return p.Page - 1 }
func (p pager) Next() int     { return p.Page + 1 }

func newPager(page, size, total int, base string) pager {
	pages := (total + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	return pager{Page: page, Pages: pages, Total: total, Size: size, Base: base}
}

func (p pager) Offset() int { return (p.Page - 1) * p.Size }
