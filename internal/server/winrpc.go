package server

import (
	"cmp"
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"ad-manager/internal/ad"
	"ad-manager/internal/audit"
	"ad-manager/internal/winrpc"

	"github.com/labstack/echo/v5"
)

// rpcClient reads Windows RPC services with the operator's credential (winrpc; tests replace it).
type rpcClient interface {
	DNSZones(ctx context.Context, t winrpc.Target, c winrpc.Cred) ([]winrpc.Zone, error)
	Records(ctx context.Context, t winrpc.Target, zone, node string, c winrpc.Cred) ([]winrpc.Record, []winrpc.Node, error)
	Sessions(ctx context.Context, t winrpc.Target, c winrpc.Cred) ([]winrpc.Session, error)
	UpdateRecord(ctx context.Context, t winrpc.Target, zone string, r winrpc.Record, del bool, c winrpc.Cred) error
	CreateZone(ctx context.Context, t winrpc.Target, zone string, c winrpc.Cred) error
}

// dnsName is a DNS zone or node name: labels of letters, digits, - and _ ("@" for the zone itself).
var dnsName = regexp.MustCompile(`^(@|[A-Za-z0-9_]([A-Za-z0-9_-]{0,62})(\.[A-Za-z0-9_]([A-Za-z0-9_-]{0,62}))*\.?)$`)

// rpcCred is the signed-in operator's credential for Kerberos, with the domain's realm and the panel's DC as KDC.
func (s *Server) rpcCred(c *echo.Context) winrpc.Cred {
	ss := sess(c)
	return winrpc.Cred{Realm: strings.ToUpper(s.cfg.AD.Domain), KDC: net.JoinHostPort(s.dcHost(), "88"), User: ss.user, Password: ss.pass}
}

// dcHost is the address of the domain controller the panel talks to (the host of ad.url).
func (s *Server) dcHost() string {
	u, err := url.Parse(s.cfg.AD.URL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// dcTarget is the panel's DC: dialed at its configured address, authenticated as its own DNS name (rootDSE).
func (s *Server) dcTarget(c *echo.Context) (winrpc.Target, error) {
	t := winrpc.Target{Addr: s.dcHost()}
	err := s.with(c, func(d directory) (err error) {
		t.Host, err = d.DCHostName()
		return err
	})
	return t, err
}

// dnsZones handles GET /api/dns/zones.
func (s *Server) dnsZones(c *echo.Context) error {
	t, err := s.dcTarget(c)
	if err != nil || committed(c) {
		return err
	}
	zones, err := s.rpc.DNSZones(c.Request().Context(), t, s.rpcCred(c))
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg(err.Error()))
	}
	return c.JSON(http.StatusOK, zones)
}

// dnsRecords handles GET /api/dns/records?zone=&node= (node "@" or empty: the zone itself).
func (s *Server) dnsRecords(c *echo.Context) error {
	zone, node := c.QueryParam("zone"), c.QueryParam("node")
	if node == "" {
		node = "@"
	}
	if zone == "@" || !dnsName.MatchString(zone) || !dnsName.MatchString(node) {
		return c.JSON(http.StatusBadRequest, msg("invalid zone or name"))
	}
	t, err := s.dcTarget(c)
	if err != nil || committed(c) {
		return err
	}
	recs, nodes, err := s.rpc.Records(c.Request().Context(), t, zone, node, s.rpcCred(c))
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg(err.Error()))
	}
	return c.JSON(http.StatusOK, map[string]any{"records": recs, "nodes": nodes})
}

// dnsChange handles POST /api/dns/records (add; JSON zone, name, type, data, ttl) and DELETE /api/dns/records
// (?zone=&name=&type=&data=): an A, CNAME or PTR record on the panel's DC, refused by ad.CheckDNSRecord when it
// belongs to the domain controllers. Audited as dns.add or dns.delete.
func (s *Server) dnsChange(del bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var in struct {
			Zone string `json:"zone"`
			Name string `json:"name"`
			Type string `json:"type"`
			Data string `json:"data"`
			TTL  uint32 `json:"ttl"`
		}
		action := map[bool]string{false: "dns.add", true: "dns.delete"}[del]
		var target string
		refuse := func(code int, err string) error { // every attempt is audited, refusals included
			audit.Log(sess(c).user, c.RealIP(), action, target, errors.New(err))
			return c.JSON(code, msg(err))
		}
		if del {
			in.Zone, in.Name, in.Type, in.Data = c.QueryParam("zone"), c.QueryParam("name"), c.QueryParam("type"), c.QueryParam("data")
		} else if err := c.Bind(&in); err != nil {
			return refuse(http.StatusBadRequest, "invalid request")
		}
		in.Type = strings.ToUpper(strings.TrimSpace(in.Type))
		in.Name, in.Data = strings.TrimSpace(in.Name), strings.TrimSpace(in.Data)
		target = in.Name + " (" + in.Zone + ") " + in.Type + " " + in.Data
		if in.Zone == "@" || !dnsName.MatchString(in.Zone) {
			return refuse(http.StatusBadRequest, "invalid zone or name")
		}
		if in.TTL == 0 {
			in.TTL = 3600
		}
		if in.TTL > 604800 {
			return refuse(http.StatusBadRequest, "TTL: 1 to 604800 seconds")
		}
		t := winrpc.Target{Addr: s.dcHost()}
		var dcs []string
		dirErr := errors.New("directory unavailable")
		err := s.with(c, func(d directory) (err error) {
			defer func() { dirErr = err }()
			if t.Host, err = d.DCHostName(); err != nil {
				return err
			}
			cs, err := d.Computers("")
			for _, k := range cs {
				if k.IsDC {
					dcs = append(dcs, k.DNSName)
				}
			}
			return err
		})
		if err != nil || committed(c) { // the directory answered with an error (s.with wrote it): audited too
			audit.Log(sess(c).user, c.RealIP(), action, target, cmp.Or(dirErr, errors.New("directory unavailable")))
			return err
		}
		name, data, err := ad.CheckDNSRecord(in.Zone, in.Name, in.Type, in.Data, append(dcs, t.Host))
		if err != nil {
			return refuse(http.StatusBadRequest, err.Error())
		}
		target = name + "." + strings.TrimSuffix(in.Zone, ".") + " " + in.Type + " " + data
		err = s.rpc.UpdateRecord(c.Request().Context(), t, in.Zone, winrpc.Record{Name: name, Type: in.Type, TTL: in.TTL, Data: data}, del, s.rpcCred(c))
		audit.Log(sess(c).user, c.RealIP(), action, target, err)
		if err != nil {
			return c.JSON(http.StatusBadGateway, msg(err.Error()))
		}
		return c.NoContent(http.StatusNoContent)
	}
}

// remoteSessions handles GET /api/computers/sessions?host=: the SMB sessions of a computer of the domain (a
// Windows server, the old DC). Only a computer account of the directory with a DNS name is called, and Kerberos
// authenticates it as that name (a forged DNS answer cannot take the operator's credential elsewhere).
func (s *Server) remoteSessions(c *echo.Context) error {
	host := strings.TrimSpace(c.QueryParam("host"))
	if host == "" || host == "@" || !dnsName.MatchString(host) {
		return c.JSON(http.StatusBadRequest, msg("invalid computer name"))
	}
	var target string
	err := s.with(c, func(d directory) error {
		cs, err := d.Computers(strings.SplitN(host, ".", 2)[0])
		if err != nil {
			return err
		}
		for _, k := range cs {
			if k.DNSName != "" && (strings.EqualFold(k.DNSName, host) || strings.EqualFold(k.Name, host)) {
				target = k.DNSName
			}
		}
		return nil
	})
	if err != nil || committed(c) {
		return err
	}
	if target == "" {
		return c.JSON(http.StatusBadRequest, msg("not a computer of the domain"))
	}
	list, err := s.rpc.Sessions(c.Request().Context(), winrpc.Target{Addr: target, Host: target}, s.rpcCred(c))
	if err != nil {
		return c.JSON(http.StatusBadGateway, msg(err.Error()))
	}
	return c.JSON(http.StatusOK, map[string]any{"host": target, "sessions": list})
}
