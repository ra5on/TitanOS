package dnsprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
)

// netcup speaks the CCP DNS API (JSON) of Netcup with customer number, API
// key and API password. Changes take several minutes to reach all name
// servers of Netcup.
type netcup struct {
	api      *httpAPI
	customer string
	key      string
	password string
}

type netcupRecord struct {
	ID           string `json:"id,omitempty"`
	Hostname     string `json:"hostname"`
	Type         string `json:"type"`
	Priority     string `json:"priority,omitempty"`
	Destination  string `json:"destination"`
	DeleteRecord bool   `json:"deleterecord,omitempty"`
}

type netcupAnswer struct {
	Status       string          `json:"status"`
	StatusCode   int             `json:"statuscode"`
	ShortMessage string          `json:"shortmessage"`
	LongMessage  string          `json:"longmessage"`
	ResponseData json.RawMessage `json:"responsedata"`
}

// netcupNoRecords is the status code for a zone without records.
const netcupNoRecords = 5029

func (n *netcup) call(ctx context.Context, action string, param map[string]any, out any) (netcupAnswer, error) {
	param["customernumber"] = n.customer
	param["apikey"] = n.key
	var answer netcupAnswer
	_, _, err := n.api.do(ctx, "Netcup", http.MethodPost, n.api.base, nil, map[string]any{"action": action, "param": param}, &answer)
	if err != nil {
		return answer, err
	}
	if answer.Status != "success" {
		message := answer.LongMessage
		if message == "" {
			message = answer.ShortMessage
		}
		return answer, &APIError{Provider: "Netcup", Message: message}
	}
	if out != nil && len(answer.ResponseData) > 0 && answer.ResponseData[0] == '{' {
		if err := json.Unmarshal(answer.ResponseData, out); err != nil {
			return answer, &APIError{Provider: "Netcup", Message: "unexpected answer"}
		}
	}
	return answer, nil
}

// session signs in, runs fn and signs out again.
func (n *netcup) session(ctx context.Context, fn func(session string) error) error {
	var login struct {
		Session string `json:"apisessionid"`
	}
	if _, err := n.call(ctx, "login", map[string]any{"apipassword": n.password}, &login); err != nil {
		return err
	}
	defer n.call(context.WithoutCancel(ctx), "logout", map[string]any{"apisessionid": login.Session}, nil)
	return fn(login.Session)
}

func (n *netcup) zone(ctx context.Context, session, fqdn string) (string, error) {
	for _, name := range candidates(fqdn) {
		if _, err := n.call(ctx, "infoDnsZone", map[string]any{"apisessionid": session, "domainname": name}, nil); err == nil {
			return name, nil
		} else if _, ok := err.(*APIError); !ok {
			return "", err
		}
	}
	return "", &APIError{Provider: "Netcup", Message: "no domain of this account holds " + fqdn}
}

func (n *netcup) records(ctx context.Context, session, zone string) ([]netcupRecord, error) {
	var data struct {
		Records []netcupRecord `json:"dnsrecords"`
	}
	answer, err := n.call(ctx, "infoDnsRecords", map[string]any{"apisessionid": session, "domainname": zone}, &data)
	if err != nil && answer.StatusCode == netcupNoRecords {
		return nil, nil
	}
	return data.Records, err
}

func (n *netcup) update(ctx context.Context, session, zone string, records []netcupRecord) error {
	if len(records) == 0 {
		return nil
	}
	_, err := n.call(ctx, "updateDnsRecords", map[string]any{"apisessionid": session, "domainname": zone, "dnsrecordset": map[string]any{"dnsrecords": records}}, nil)
	return err
}

// change signs in, finds the zone of fqdn and lets fn decide on the records.
func (n *netcup) change(ctx context.Context, fqdn string, fn func(host string, existing []netcupRecord) []netcupRecord) error {
	return n.session(ctx, func(session string) error {
		zone, err := n.zone(ctx, session, fqdn)
		if err != nil {
			return err
		}
		all, err := n.records(ctx, session, zone)
		if err != nil {
			return err
		}
		host := relative(fqdn, zone)
		var mine []netcupRecord
		for _, r := range all {
			if r.Hostname == host {
				mine = append(mine, r)
			}
		}
		return n.update(ctx, session, zone, fn(host, mine))
	})
}

func (n *netcup) SetTXT(ctx context.Context, fqdn string, values []string) error {
	return n.change(ctx, fqdn, func(host string, existing []netcupRecord) []netcupRecord {
		have := map[string]bool{}
		for _, r := range existing {
			if r.Type == "TXT" {
				have[r.Destination] = true
			}
		}
		var out []netcupRecord
		for _, value := range values {
			if !have[value] {
				out = append(out, netcupRecord{Hostname: host, Type: "TXT", Destination: value})
			}
		}
		return out
	})
}

func (n *netcup) ClearTXT(ctx context.Context, fqdn string) error {
	return n.change(ctx, fqdn, func(host string, existing []netcupRecord) []netcupRecord {
		var out []netcupRecord
		for _, r := range existing {
			if r.Type == "TXT" {
				r.DeleteRecord = true
				out = append(out, r)
			}
		}
		return out
	})
}

func (n *netcup) MultipleTXT() bool { return true }

func (n *netcup) SetAddresses(ctx context.Context, host string, v4, v6 netip.Addr) error {
	return n.change(ctx, host, func(name string, existing []netcupRecord) []netcupRecord {
		var out []netcupRecord
		for _, a := range addressRecords(v4, v6) {
			found := false
			for _, r := range existing {
				if r.Type == a.kind {
					found = true
					if r.Destination != a.value {
						r.Destination = a.value
						out = append(out, r)
					}
				}
			}
			if !found {
				out = append(out, netcupRecord{Hostname: name, Type: a.kind, Destination: a.value})
			}
		}
		return out
	})
}
