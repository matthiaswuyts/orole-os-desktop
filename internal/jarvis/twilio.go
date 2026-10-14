package jarvis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Twilio struct {
	SID, Token, BaseURL string
	HTTP                *http.Client
}

// do sends one authenticated request. Errors are fixed sentences: they never echo the token, the URL or the body.
func (t *Twilio) do(ctx context.Context, method, path string, form url.Values) (*http.Response, error) {
	base := strings.TrimRight(t.BaseURL, "/")
	if base == "" {
		base = "https://api.twilio.com"
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, base+"/2010-04-01/Accounts/"+url.PathEscape(t.SID)+path, body)
	if err != nil {
		return nil, errors.New("Twilio request could not be built; check the Account SID.")
	}
	req.SetBasicAuth(t.SID, t.Token)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	hc := t.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		return nil, errors.New("Could not reach Twilio.")
	}
	if res.StatusCode == http.StatusUnauthorized {
		res.Body.Close()
		return nil, errors.New("Twilio rejected the Account SID or auth token.")
	}
	if res.StatusCode >= 300 {
		res.Body.Close()
		return nil, fmt.Errorf("Twilio returned %d.", res.StatusCode)
	}
	return res, nil
}

// callPath is the REST path of one call; the SID is checked so it cannot change the path.
func callPath(callSID string) (string, error) {
	if !callSIDRE.MatchString(callSID) {
		return "", errors.New("That is not a Twilio call id.")
	}
	return "/Calls/" + callSID + ".json", nil
}

var callSIDRE = regexp.MustCompile(`^CA[0-9a-fA-F]{32}$`)

// Hangup ends a live call at the carrier (Status=completed). Ending a call that already ended is harmless.
func (t *Twilio) Hangup(ctx context.Context, callSID string) error {
	p, err := callPath(callSID)
	if err != nil {
		return err
	}
	res, err := t.do(ctx, "POST", p, url.Values{"Status": {"completed"}})
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

// CallStatus returns Twilio's status for a call: queued, ringing, in-progress, completed, busy, no-answer,
// failed or canceled.
func (t *Twilio) CallStatus(ctx context.Context, callSID string) (string, error) {
	p, err := callPath(callSID)
	if err != nil {
		return "", err
	}
	res, err := t.do(ctx, "GET", p, nil)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return "", errors.New("Twilio returned an unreadable response.")
	}
	return body.Status, nil
}

// Numbers lists the account's incoming numbers; a 401 means the SID or token is wrong.
func (t *Twilio) Numbers(ctx context.Context) ([]string, error) {
	res, err := t.do(ctx, "GET", "/IncomingPhoneNumbers.json?PageSize=1000", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var body struct {
		Numbers []struct {
			PhoneNumber string `json:"phone_number"`
		} `json:"incoming_phone_numbers"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return nil, errors.New("Twilio returned an unreadable response.")
	}
	out := make([]string, 0, len(body.Numbers))
	for _, n := range body.Numbers {
		out = append(out, n.PhoneNumber)
	}
	return out, nil
}
