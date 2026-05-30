package api

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

const (
	defaultPageSize = 100
	maxPageSize     = 500
)

type page struct {
	start         int
	size          int
	nextPageToken string
	totalSize     int
}

func resourceName(collection, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, collection+"/") {
		return id
	}
	return collection + "/" + id
}

func resourceID(collection, name string) string {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, collection+"/") {
		return strings.TrimPrefix(name, collection+"/")
	}
	return name
}

func resourceIDs(collection string, names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if id := resourceID(collection, name); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func resourceNames(collection string, ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if name := resourceName(collection, id); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func parsePagination(c echo.Context, total int) (page, error) {
	start, size, err := parsePageRequest(c)
	if err != nil {
		return page{}, err
	}
	if start > total {
		start = total
	}
	next := ""
	if start+size < total {
		next = encodePageToken(start + size)
	}
	return page{start: start, size: size, nextPageToken: next, totalSize: total}, nil
}

func parsePageRequest(c echo.Context) (int, int, error) {
	size := defaultPageSize
	if raw := strings.TrimSpace(c.QueryParam("pageSize")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, 0, fmt.Errorf("pageSize must be a non-negative integer")
		}
		if n > 0 {
			size = n
		}
	}
	if size > maxPageSize {
		size = maxPageSize
	}

	start := 0
	if token := strings.TrimSpace(c.QueryParam("pageToken")); token != "" {
		offset, err := decodePageToken(token)
		if err != nil || offset < 0 {
			return 0, 0, fmt.Errorf("pageToken is invalid")
		}
		start = offset
	}
	return start, size, nil
}

func paginate[T any](items []T, p page) []T {
	if p.start >= len(items) {
		return []T{}
	}
	end := p.start + p.size
	if end > len(items) {
		end = len(items)
	}
	return items[p.start:end]
}

func encodePageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodePageToken(token string) (int, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(decoded))
}

func stringFilter(c echo.Context, name string) string {
	return strings.TrimSpace(c.QueryParam(name))
}

func matchFilter(value, filter string) bool {
	return filter == "" || strings.EqualFold(strings.TrimSpace(value), filter)
}

func splitCustomMethod(path string) (string, string, bool) {
	idx := strings.LastIndex(path, ":")
	if idx <= 0 || idx == len(path)-1 {
		return "", "", false
	}
	return path[:idx], path[idx+1:], true
}

func withNameParam(c echo.Context, name string, next echo.HandlerFunc) error {
	c.SetParamNames("name")
	c.SetParamValues(name)
	return next(c)
}
