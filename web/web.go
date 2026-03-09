// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Configuration and functions for the event server web interface.

package web

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
)

const (
	sessionTokenCookie = "session_token"
	adminUser          = "admin"
)

type Web struct {
	arena           *field.Arena
	templateHelpers template.FuncMap
}

func NewWeb(arena *field.Arena) *Web {
	web := &Web{arena: arena}

	// Helper functions that can be used inside templates.
	web.templateHelpers = template.FuncMap{
		// Allows sub-templates to be invoked with multiple arguments.
		"dict": func(values ...any) (map[string]any, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("Invalid dict call.")
			}
			dict := make(map[string]any, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("Dict keys must be strings.")
				}
				dict[key] = values[i+1]
			}
			return dict, nil
		},
		"add": func(a, b int) int {
			return a + b
		},
		"itoa": func(a int) string {
			return strconv.Itoa(a)
		},
		"multiply": func(a, b int) int {
			return a * b
		},
		"seq": func(count int) []int {
			seq := make([]int, count)
			for i := 0; i < count; i++ {
				seq[i] = i + 1
			}
			return seq
		},
		"toUpper": func(str string) string {
			return strings.ToUpper(str)
		},

		// MatchType enum values.
		"testMatch":          model.Test.Get,
		"practiceMatch":      model.Practice.Get,
		"qualificationMatch": model.Qualification.Get,
		"playoffMatch":       model.Playoff.Get,

		// MatchStatus enum values.
		"matchScheduled": game.MatchScheduled.Get,
	}

	return web
}

// Starts the webserver and blocks, waiting on requests. Does not return until the application exits.
func (web *Web) ServeWebInterface(port int) {
	http.Handle("/static/", http.StripPrefix("/static/", addNoCacheHeader(http.FileServer(http.Dir("static/")))))
	http.Handle("/", web.newHandler())
	log.Printf("Serving HTTP requests on port %d", port)

	// Start Server
	http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
}

// Adds a "Cache-Control: no-cache" header to the given handler to force browser validation of last modified time.
func addNoCacheHeader(handler http.Handler) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Cache-Control", "no-cache")
			handler.ServeHTTP(w, r)
		},
	)
}

// Sets up the mapping between URLs and handlers.
func (web *Web) newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/free_practice", http.StatusFound)
	})
	mux.HandleFunc("GET /login", web.loginHandler)
	mux.HandleFunc("POST /login", web.loginPostHandler)
	mux.HandleFunc("GET /match_play", web.matchPlayHandler)
	mux.HandleFunc("GET /match_play/websocket", web.matchPlayWebsocketHandler)
	mux.HandleFunc("GET /free_practice", web.freePracticeHandler)
	mux.HandleFunc("GET /free_practice/websocket", web.freePracticeWebsocketHandler)
	mux.HandleFunc("POST /setup/db/clear/{type}", web.clearDbHandler)
	mux.HandleFunc("POST /setup/db/restore", web.restoreDbHandler)
	mux.HandleFunc("GET /setup/db/save", web.saveDbHandler)
	mux.HandleFunc("GET /setup/settings", web.settingsGetHandler)
	mux.HandleFunc("POST /setup/settings", web.settingsPostHandler)
	return mux
}

// Writes the given error out as plain text with a status code of 500.
func handleWebErr(w http.ResponseWriter, err error) {
	log.Printf("HTTP request error: %v", err)
	http.Error(w, "Internal server error: "+err.Error(), 500)
}

// Prepends the base directory to the template filenames.
func (web *Web) parseFiles(filenames ...string) (*template.Template, error) {
	var paths []string
	for _, filename := range filenames {
		paths = append(paths, filepath.Join(model.BaseDir, filename))
	}

	template := template.New("").Funcs(web.templateHelpers)
	return template.ParseFiles(paths...)
}
