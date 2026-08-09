package httpx

import (
	"net/http"
	"strconv"
)

// Page describes pagination parameters.
type Page struct {
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
	Offset   int `json:"-"`
}

// PageResult wraps a page of items.
type PageResult struct {
	Items    any `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

// ParsePage reads page/pageSize query params (defaults: 1/20, max 100).
func ParsePage(r *http.Request) Page {
	page := parsePositive(r, "page", 1)
	size := parsePositive(r, "pageSize", 20)
	if size > 100 {
		size = 100
	}
	return Page{Page: page, PageSize: size, Offset: (page - 1) * size}
}

func parsePositive(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}

// PageOf builds a PageResult.
func PageOf(items any, total int, p Page) PageResult {
	return PageResult{Items: items, Total: total, Page: p.Page, PageSize: p.PageSize}
}
