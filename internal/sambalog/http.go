package sambalog

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"
)

// ListHandler serves GET …/samba/logs: the directory and its log files.
// A missing or unreadable directory is not an error for the page: it answers
// 200 with "error" so the tab can explain (e.g. panel not on a Samba host).
func ListHandler(dir string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		files, err := List(dir)
		res := map[string]any{"dir": dir, "files": files}
		if err != nil {
			res["files"], res["error"] = []File{}, err.Error()
		}
		return c.JSON(http.StatusOK, res)
	}
}

// ReadHandler serves GET …/samba/logs/:name?lines=&q=: newest entries first.
func ReadHandler(dir, param string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		n, _ := strconv.Atoi(c.QueryParam("lines"))
		if n <= 0 || n > 5000 {
			n = 500
		}
		entries, err := Read(dir, c.Param(param), n, c.QueryParam("q"))
		switch {
		case errors.Is(err, ErrInvalidName):
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		case err != nil:
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, entries)
	}
}
