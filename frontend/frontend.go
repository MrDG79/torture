package main

import (
	"github.com/flosch/pongo2"
	"github.com/julienschmidt/httprouter"
	"golang.org/x/net/http2"
	"io"
	"log"
	"net/http"
)

type FrontendConfig struct {
	HttpListen    string
	ElasticServer string
	LogOutput     io.Writer
	PerPage       int

	TLSListen string
	TLSCert   string
	TLSKey    string
}

type Frontend struct {
	cfg           FrontendConfig
	elasticSearch *ElasticSearch
	templates     *pongo2.TemplateSet

	Log *log.Logger
}

func CreateFrontend(cfg FrontendConfig) (frontend *Frontend, err error) {
	frontend = &Frontend{cfg: cfg}

	// Create logger
	frontend.Log = log.New(frontend.cfg.LogOutput, "frontend: ", log.Ldate|log.Lshortfile)

	// Create an ElasticSearch connection
	frontend.elasticSearch, err = CreateElasticSearch(frontend.cfg.ElasticServer)
	if err != nil {
		return
	}

	// Create a pongo2 template set
	frontend.templates = pongo2.NewSet("torture")
	frontend.templates.SetBaseDirectory("templates")

	// Sub-Apps
	errorCatcher, err := CreateErrorCatcher(ErrorCatcherConfig{
		Frontend: frontend,
	})
	if err != nil {
		return
	}

	search, err := CreateSearch(SearchConfig{
		Frontend: frontend,
	})
	if err != nil {
		return
	}

	mux := httprouter.New()
	mux.Handle("GET", "/s", errorCatcher.Handler(search.Handler))
	mux.Handler("GET", "/", http.RedirectHandler("/s", 301))
	mux.ServeFiles("/static/*filepath", http.Dir("static"))

	srv := &http.Server{
		Addr:    frontend.cfg.HttpListen,
		Handler: mux,
	}
	http2.ConfigureServer(srv, &http2.Server{})

	// Plain HTTP only if TLS is not configured properly!
	if frontend.cfg.TLSCert == "" || frontend.cfg.TLSKey == "" {
		err = srv.ListenAndServe()
		return
	}

	// Redirect plain HTTP to TLS
	// TODO make this work if TLS is not on port 443
	redirect := http.NewServeMux()
	redirect.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := r.URL
		url.Host = r.Host
		url.Scheme = "https"

		http.Redirect(w, r, url.String(), http.StatusMovedPermanently)
	})
	srv.Handler = redirect

	err = srv.ListenAndServe()
	if err != nil {
		return
	}

	// Serve HTTP via TLS
	tlsSrv := &http.Server{
		Addr:    frontend.cfg.TLSListen,
		Handler: mux,
	}
	http2.ConfigureServer(tlsSrv, &http2.Server{})

	err = tlsSrv.ListenAndServeTLS(frontend.cfg.TLSCert, frontend.cfg.TLSKey)
	return
}
