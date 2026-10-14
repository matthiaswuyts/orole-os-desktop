package jarvis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Twilio struct {
	SID, Token, BaseURL string
	HTTP                *http.Client
}

// Numbers lists the account's incoming numbers; a 401 means the SID or token is wrong.
func (t *Twilio) Numbers(ctx context.Context) ([]string, error) {
	base := strings.TrimRight(t.BaseURL, "/")
	if base == "" {
		base = "https://api.twilio.com"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/2010-04-01/Accounts/"+url.PathEscape(t.SID)+"/IncomingPhoneNumbers.json?PageSize=1000", nil)
	if err != nil {
		return nil, errors.New("Twilio request could not be built; check the Account SID.")
	}
	req.SetBasicAuth(t.SID, t.Token)
	hc := t.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		return nil, errors.New("Could not reach Twilio.")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("Twilio rejected the Account SID or auth token.")
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("Twilio returned %d.", res.StatusCode)
	}
	var body struct {
		Numbers []struct {
			PhoneNumber string `json:"phone_number"`
		} `json:"incoming_phone_numbers"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, errors.New("Twilio returned an unreadable response.")
	}
	out := make([]string, 0, len(body.Numbers))
	for _, n := range body.Numbers {
		out = append(out, n.PhoneNumber)
	}
	return out, nil
}
