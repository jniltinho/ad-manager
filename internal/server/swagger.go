package server

import (
	_ "embed"
	"net/http"

	"github.com/labstack/echo/v5"
)

// openAPI documents the panel API (served at /swagger/openapi.yaml).
//
//go:embed openapi.yaml
var openAPI []byte

// The page loads only same-origin files (CSP default-src 'self'): the Swagger UI
// bundle and CSS copied by the frontend build into web/dist/swagger, and init.js.
const swaggerPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>AD Manager API</title>
<link rel="stylesheet" href="/swagger/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="/swagger/swagger-ui-bundle.js"></script>
<script src="/swagger/init.js"></script>
</body>
</html>
`

const swaggerInit = `window.ui = SwaggerUIBundle({
  url: '/swagger/openapi.yaml',
  dom_id: '#swagger-ui',
  deepLinking: true,
  persistAuthorization: false,
  // the session cookie (adc_session) goes along: sign in with POST /api/login first
  requestInterceptor: (r) => { r.credentials = 'same-origin'; return r },
})
`

// swagger registers /swagger: switched on in Settings (off by default) and,
// like /asynqmon, only for a logged-in administrator.
func (s *Server) swagger(e *echo.Echo) {
	on := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if !s.settings().Swagger {
				return c.JSON(http.StatusNotFound, msg("not found"))
			}
			return next(c)
		}
	}
	page := func(c *echo.Context) error { return c.HTML(http.StatusOK, swaggerPage) }
	e.GET("/swagger", page, on, s.authPage)
	e.GET("/swagger/", page, on, s.authPage)
	e.GET("/swagger/init.js", func(c *echo.Context) error {
		return c.Blob(http.StatusOK, "text/javascript; charset=utf-8", []byte(swaggerInit))
	}, on, s.authPage)
	e.GET("/swagger/openapi.yaml", func(c *echo.Context) error {
		return c.Blob(http.StatusOK, "application/yaml; charset=utf-8", openAPI)
	}, on, s.authPage)
}
