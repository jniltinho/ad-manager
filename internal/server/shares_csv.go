package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"ad-manager/internal/ad"
	"ad-manager/internal/agent"
	"ad-manager/internal/audit"
	"ad-manager/internal/shares"
	"ad-manager/internal/store"

	"github.com/labstack/echo/v5"
)

// agentAndShares loads file server :id and its current share list. On
// failure it has already written the response and returns a nil agent.
func (s *Server) agentAndShares(c *echo.Context) (*store.Agent, []shares.Share, error) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	return s.agentSharesByID(c, id)
}

// agentSharesByID is agentAndShares for the agent id (on an error it has answered the request).
func (s *Server) agentSharesByID(c *echo.Context, id int64) (*store.Agent, []shares.Share, error) {
	if s.store == nil {
		return nil, nil, noStore(c)
	}
	a, err := s.store.Agent(id)
	if err != nil {
		return nil, nil, c.JSON(http.StatusNotFound, msg("server not registered"))
	}
	st, out, err := agentDo(c.Request().Context(), a, http.MethodGet, a.URL+"/api/shares", nil)
	if err != nil || st != http.StatusOK {
		return nil, nil, c.JSON(http.StatusBadGateway, msg(fmt.Sprintf("server %s unavailable: %v %s", a.Name, err, out)))
	}
	var list []shares.Share
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, nil, c.JSON(http.StatusBadGateway, msg("invalid response from the agent"))
	}
	return a, list, nil
}

// sendShares queues one share task per action of plan on the agent (skips are left alone), auditing each.
func sendShares(c *echo.Context, a *store.Agent, plan []ad.Action, reqs map[string]shares.ShareReq) {
	for i := range plan {
		p := &plan[i]
		if p.Kind == "skip" {
			p.Result = "skipped"
			continue
		}
		payload, _ := json.Marshal(reqs[strings.ToLower(p.Target)])
		body, _ := json.Marshal(agent.TaskReq{Type: agent.TypeShare, Payload: payload})
		st, out, err := agentDo(c.Request().Context(), a, http.MethodPost, a.URL+"/api/tasks", body)
		if err == nil && st >= 300 {
			var e struct{ Error string }
			_ = json.Unmarshal(out, &e)
			err = fmt.Errorf("%s", strings.TrimSpace(e.Error+" "+http.StatusText(st)))
		}
		p.OK, p.Result = err == nil, "queued"
		target := a.Name + ":" + p.Target
		if err != nil {
			p.Result = err.Error()
		} else {
			var ti agent.TaskInfo
			_ = json.Unmarshal(out, &ti)
			p.Result = "queued (" + ti.ID + ")"
			target += " task " + ti.ID // the agent's task log carries the outcome under this id
		}
		audit.Log(sess(c).user, c.RealIP(), "import."+p.Kind, target, err)
	}
}

// rowMaps turns CSV rows into the column → value maps planShares reads.
func rowMaps(header []string, rows [][]string) []map[string]string {
	out := make([]map[string]string, 0, len(rows))
	for _, r := range rows {
		m := map[string]string{}
		for i, h := range header {
			if i < len(r) {
				m[h] = r[i]
			}
		}
		out = append(out, m)
	}
	return out
}

// exportShares handles GET /api/agents/:id/export: the server's folders and
// permissions as CSV (no files).
func (s *Server) exportShares(c *echo.Context) error {
	a, list, err := s.agentAndShares(c)
	if a == nil {
		return err
	}
	rows := make([][]string, 0, len(list))
	for _, sh := range list {
		rows = append(rows, shares.CSVRow(sh))
	}
	h := c.Response().Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="shares.csv"`)
	c.Response().WriteHeader(http.StatusOK)
	return ad.WriteCSV(c.Response(), shares.CSVHeader, rows)
}

// planShares compares the CSV rows with what the server already publishes, and returns the request of each row
// (by lowercase name). An option column missing from the file (an older export) keeps the share's current value.
func planShares(rows []map[string]string, current []shares.Share) ([]ad.Action, map[string]shares.ShareReq) {
	byName := map[string]shares.Share{}
	for _, sh := range current {
		byName[strings.ToLower(sh.Name)] = sh
	}
	sameSet := func(a, b []string) bool {
		a, b = slices.Clone(a), slices.Clone(b)
		slices.Sort(a)
		slices.Sort(b)
		return slices.EqualFunc(a, b, strings.EqualFold)
	}
	// parents before children: a nested share's folder depends on its parent's
	rows = slices.Clone(rows)
	slices.SortStableFunc(rows, func(x, y map[string]string) int {
		return strings.Count(x["path"], "/") - strings.Count(y["path"], "/")
	})
	var plan []ad.Action
	reqs := map[string]shares.ShareReq{}
	for _, r := range rows {
		req := shares.FromCSV(r)
		cur, ok := byName[strings.ToLower(req.Name)]
		if ok {
			for col, v := range map[string]*bool{"recycle_bin": &req.Recycle, "abe": &req.ABE, "audit": &req.Audit} {
				if _, given := r[col]; !given {
					*v = map[string]bool{"recycle_bin": cur.Recycle, "abe": cur.ABE, "audit": cur.Audit}[col]
				}
			}
		}
		reqs[strings.ToLower(req.Name)] = req
		if !ok {
			plan = append(plan, ad.Action{Kind: "share.create", Target: req.Name, Detail: req.Path})
			continue
		}
		var diff []string
		if cur.Path != req.Path {
			diff = append(diff, fmt.Sprintf("path: %s → %s", cur.Path, req.Path))
		}
		for _, d := range []struct {
			label     string
			cur, want []string
		}{{"full control", cur.Full, req.Full}, {"write", shares.Without(cur.RW, cur.Full), req.RW}, {"read", cur.RO, req.RO},
			{"full control (users)", cur.FullUsers, req.FullUsers}, {"write (users)", shares.Without(cur.RWUsers, cur.FullUsers), req.RWUsers},
			{"read (users)", cur.ROUsers, req.ROUsers}} {
			if !sameSet(d.cur, d.want) {
				diff = append(diff, fmt.Sprintf("%s: %s → %s", d.label, strings.Join(d.cur, ", "), strings.Join(d.want, ", ")))
			}
		}
		for _, o := range []struct {
			label     string
			cur, want bool
		}{{"recycle bin", cur.Recycle, req.Recycle}, {"ABE", cur.ABE, req.ABE}, {"audit", cur.Audit, req.Audit}} {
			if o.cur != o.want {
				diff = append(diff, fmt.Sprintf("%s: %v → %v", o.label, o.cur, o.want))
			}
		}
		if len(diff) == 0 {
			plan = append(plan, ad.Action{Kind: "skip", Target: req.Name, Detail: "same as on the server"})
			continue
		}
		plan = append(plan, ad.Action{Kind: "share.update", Target: req.Name, Detail: strings.Join(diff, "; ")})
	}
	return plan, reqs
}

// importShares handles POST /api/agents/:id/import/plan|apply: the multipart
// field "shares" becomes a plan; with apply it enqueues one share:create per
// changed row (the agent creates the empty folder and the ACLs).
func (s *Server) importShares(apply bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		fh, err := c.FormFile("shares")
		if err != nil {
			return c.JSON(http.StatusBadRequest, msg("send shares.csv"))
		}
		f, err := fh.Open()
		if err != nil {
			return fail(c, err)
		}
		rows, err := ad.ReadCSV(f, shares.CSVHeader)
		f.Close()
		if err != nil {
			return fail(c, err)
		}
		a, current, err := s.agentAndShares(c)
		if a == nil {
			return err
		}
		plan, reqs := planShares(rows, current)
		if apply {
			sendShares(c, a, plan, reqs)
		}
		return c.JSON(http.StatusOK, map[string]any{"plan": plan})
	}
}
