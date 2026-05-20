package github

import (
	"errors"
	"net/http"
	"net/url"
)

// absoluteLocation resolves the Location header on resp into an absolute URL,
// using the original request URL as the base.
func absoluteLocation(resp *http.Response, requestURL string) (string, error) {
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", errors.New("no Location header on redirect")
	}
	base, err := url.Parse(requestURL)
	if err != nil {
		return "", err
	}
	abs, err := base.Parse(loc)
	if err != nil {
		return "", err
	}
	return abs.String(), nil
}
