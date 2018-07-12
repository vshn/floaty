package main

import (
	"net/url"
)

// Wrapper de-/serializing URLs from/to text
type textURL struct {
	url.URL
}

func (u textURL) String() string {
	return u.URL.String()
}

func (u textURL) MarshalText() ([]byte, error) {
	return []byte(u.String()), nil
}

func (u *textURL) UnmarshalText(text []byte) error {
	parsed, err := url.Parse(string(text))
	if err != nil {
		return err
	}

	*u = textURL{*parsed}

	return nil
}

func parseTextURL(text string) (textURL, error) {
	result := textURL{}

	if err := result.UnmarshalText([]byte(text)); err != nil {
		return result, err
	}

	return result, nil
}

func mustParseTextURL(text string) textURL {
	result := textURL{}

	if err := result.UnmarshalText([]byte(text)); err != nil {
		panic(err)
	}

	return result
}
