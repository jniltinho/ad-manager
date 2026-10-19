package server

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"ad-manager/internal/audit"

	"github.com/labstack/echo/v5"
	emw "github.com/labstack/echo/v5/middleware"
)

// logs handles GET /api/audit?q=&limit= (logged-in administrator only).
func (s *Server) logs(c *echo.Context) error {
	if s.cfg.Server.AuditLog == "" {
		return c.JSON(http.StatusNotFound, msg("the audit log has no file (server.audit_log is empty): events go to the service journal"))
	}
	n, _ := strconv.Atoi(c.QueryParam("limit"))
	if n <= 0 || n > 5000 {
		n = 1000
	}
	ev, err := audit.Read(s.cfg.Server.AuditLog, n, c.QueryParam("q"))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, msg("could not read the audit log: "+err.Error()))
	}
	return c.JSON(http.StatusOK, ev)
}

// requestLog writes each API request (and /asynqmon) to the access log: time,
// IP, operator, method, path, status, duration and size — never bodies or
// headers. Static files of the SPA are skipped.
func (s *Server) requestLog() echo.MiddlewareFunc {
	return emw.RequestLoggerWithConfig(emw.RequestLoggerConfig{
		Skipper: func(c *echo.Context) bool {
			p := c.Request().URL.Path
			return !strings.HasPrefix(p, "/api/") && !strings.HasPrefix(p, "/asynqmon")
		},
		HandleError: true, LogStatus: true, LogURI: true, LogMethod: true, LogLatency: true,
		LogRemoteIP: true, LogResponseSize: true,
		LogValuesFunc: func(c *echo.Context, v emw.RequestLoggerValues) error {
			r := audit.Request{IP: v.RemoteIP, Method: v.Method, Path: redactPath(v.URI), Status: v.Status,
				Ms: float64(v.Latency.Microseconds()) / 1000, Bytes: v.ResponseSize}
			if se, ok := c.Get("sess").(*session); ok {
				r.User = se.user
			}
			if v.Error != nil {
				r.Error = v.Error.Error()
			}
			audit.Access(r)
			return nil
		},
	})
}

// redactPath hides the token of a password link: whoever reads the log must not
// be able to use it.
func redactPath(uri string) string {
	const link = "/api/password-link/"
	if strings.HasPrefix(uri, link) {
		return link + "***"
	}
	return uri
}

// httpLog handles GET /api/http-log?q=&limit= (logged-in administrator only).
func (s *Server) httpLog(c *echo.Context) error {
	if s.cfg.Server.AccessLog == "" {
		return c.JSON(http.StatusNotFound, msg("HTTP log disabled (server.access_log is empty)"))
	}
	n, _ := strconv.Atoi(c.QueryParam("limit"))
	if n <= 0 || n > 5000 {
		n = 1000
	}
	rs, err := audit.ReadAccess(s.cfg.Server.AccessLog, n, c.QueryParam("q"))
	if errors.Is(err, os.ErrNotExist) {
		return c.JSON(http.StatusOK, []audit.Request{})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, msg("could not read the HTTP log: "+err.Error()))
	}
	return c.JSON(http.StatusOK, rs)
}
