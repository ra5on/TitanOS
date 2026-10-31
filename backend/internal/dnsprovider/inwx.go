package dnsprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
)

// inwx speaks the JSON-RPC API (DomRobot) of INWX with user name and
// password. Accounts with two-factor sign-in are not supported: the API
// would ask for a code every time.
type inwx struct {
	api      *httpAPI
	user     string
	password string
}

type inwxRecord struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
}

// inwxSession is one sign-in; the API keeps it in a cookie.
type inwxSession struct {
	api *httpAPI
}

func (s *inwxSession) call(ctx context.Context, method string, params map[string]any, out any) (int, error) {
	var answer struct {
		Code    int             `json:"code"`
		Msg     string          `json:"msg"`
		Reason  string          `json:"reason"`
		ResData json.RawMessage `json:"resData"`
	}
	if _, _, err := s.api.do(ctx, "INWX", http.MethodPost, s.api.base, nil, map[string]any{"method": method, "params": params}, &answer); err != nil {
		return 0, err
	}
	if answer.Code != 1000 && answer.Code != 1001 {
		message := answer.Msg
		if answer.Reason != "" {
			message += " (" + answer.Reason + ")"
		}
		return answer.Code, &APIError{Provider: "INWX", Message: fmt.Sprintf("%d %s", answer.Code, message)}
	}
	if out != nil && len(answer.ResData) > 0 {
		if err := json.Unmarshal(answer.ResData, out); err != nil {
			return answer.Code, &APIError{Provider: "INWX", Message: "unexpected answer"}
		}
	}
	return answer.Code, nil
}

func (i *inwx) session(ctx context.Context, fn func(s *inwxSession) error) error {
	jar, _ := cookiejar.New(nil)
	client := *i.api.client
	client.Jar = jar
	s := &inwxSession{api: &httpAPI{client: &client, base: i.api.base}}
	var login struct {
		TFA any `json:"tfa"`
	}
	if _, err := s.call(ctx, "account.login", map[string]any{"user": i.user, "pass": i.password}, &login); err != nil {
		return err
	}
	// Logging out is best effort: the session expires on its own.
	defer func() { _, _ = s.call(context.WithoutCancel(ctx), "account.logout", map[string]any{}, nil) }()
	if tfa, ok := login.TFA.(string); ok && tfa != "" && tfa != "0" {
		return &APIError{Provider: "INWX", Message: "the account asks for a second factor, which TitanOS cannot give; use an account without two-factor sign-in"}
	}
	return fn(s)
}

func (s *inwxSession) zone(ctx context.Context, fqdn string) (string, error) {
	for _, name := range candidates(fqdn) {
		var info struct {
			Domain string `json:"domain"`
		}
		if _, err := s.call(ctx, "nameserver.info", map[string]any{"domain": name, "type": "SOA"}, &info); err == nil {
			return name, nil
		} else if _, ok := err.(*APIError); !ok {
			return "", err
		}
	}
	return "", &APIError{Provider: "INWX", Message: "no domain of this account holds " + fqdn}
}

func (s *inwxSession) records(ctx context.Context, zone, name, kind string) ([]inwxRecord, error) {
	var info struct {
		Records []inwxRecord `json:"record"`
	}
	_, err := s.call(ctx, "nameserver.info", map[string]any{"domain": zone, "name": name, "type": kind}, &info)
	var out []inwxRecord
	for _, r := range info.Records {
		if r.Type == kind && r.Name == name {
			out = append(out, r)
		}
	}
	return out, err
}

func (i *inwx) SetTXT(ctx context.Context, fqdn string, values []string) error {
	return i.session(ctx, func(s *inwxSession) error {
		zone, err := s.zone(ctx, fqdn)
		if err != nil {
			return err
		}
		existing, err := s.records(ctx, zone, fqdn, "TXT")
		if err != nil {
			return err
		}
		have := map[string]bool{}
		for _, r := range existing {
			have[r.Content] = true
		}
		for _, value := range values {
			if have[value] {
				continue
			}
			if _, err := s.call(ctx, "nameserver.createRecord", map[string]any{"domain": zone, "type": "TXT", "name": fqdn, "content": value, "ttl": 300}, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

func (i *inwx) ClearTXT(ctx context.Context, fqdn string) error {
	return i.session(ctx, func(s *inwxSession) error {
		zone, err := s.zone(ctx, fqdn)
		if err != nil {
			return err
		}
		existing, err := s.records(ctx, zone, fqdn, "TXT")
		if err != nil {
			return err
		}
		for _, r := range existing {
			if _, err := s.call(ctx, "nameserver.deleteRecord", map[string]any{"id": r.ID}, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

func (i *inwx) MultipleTXT() bool { return true }

func (i *inwx) SetAddresses(ctx context.Context, host string, v4, v6 netip.Addr) error {
	return i.session(ctx, func(s *inwxSession) error {
		zone, err := s.zone(ctx, host)
		if err != nil {
			return err
		}
		for _, a := range addressRecords(v4, v6) {
			existing, err := s.records(ctx, zone, host, a.kind)
			if err != nil {
				return err
			}
			if len(existing) == 0 {
				if _, err := s.call(ctx, "nameserver.createRecord", map[string]any{"domain": zone, "type": a.kind, "name": host, "content": a.value, "ttl": 300}, nil); err != nil {
					return err
				}
				continue
			}
			for _, r := range existing {
				if r.Content == a.value {
					continue
				}
				if _, err := s.call(ctx, "nameserver.updateRecord", map[string]any{"id": r.ID, "content": a.value}, nil); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
