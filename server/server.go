package server

import (
	"crypto/subtle"
	"github.com/0x10240/mihomo-proxy-pool/proxypool"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/metacubex/mihomo/adapter/inbound"
	"github.com/metacubex/mihomo/common/utils"
	"github.com/metacubex/mihomo/log"
	"github.com/sagernet/cors"
	"net/http"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
)

var (
	httpServer *http.Server
)

type Config struct {
	Addr        string
	TLSAddr     string
	UnixAddr    string
	PipeAddr    string
	Secret      string
	Certificate string
	PrivateKey  string
	DohServer   string
	IsDebug     bool
	Cors        Cors
}

type Cors struct {
	AllowOrigins        []string
	AllowPrivateNetwork bool
}

func (c Cors) Apply(r chi.Router) {
	r.Use(cors.New(cors.Options{
		AllowedOrigins:      c.AllowOrigins,
		AllowedMethods:      []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowedHeaders:      []string{"Content-Type", "Authorization"},
		AllowPrivateNetwork: c.AllowPrivateNetwork,
		MaxAge:              300,
	}).Handler)
}

func safeEuqal(a, b string) bool {
	aBuf := utils.ImmutableBytesFromString(a)
	bBuf := utils.ImmutableBytesFromString(b)
	return subtle.ConstantTimeCompare(aBuf, bBuf) == 1
}

func authentication(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			// Browser websocket not support custom header
			if r.Header.Get("Upgrade") == "websocket" && r.URL.Query().Get("token") != "" {
				token := r.URL.Query().Get("token")
				if !safeEuqal(token, secret) {
					render.Status(r, http.StatusUnauthorized)
					render.JSON(w, r, ErrUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			header := r.Header.Get("Authorization")
			bearer, token, found := strings.Cut(header, " ")

			hasInvalidHeader := bearer != "Bearer"
			hasInvalidSecret := !found || !safeEuqal(token, secret)
			if hasInvalidHeader || hasInvalidSecret {
				render.Status(r, http.StatusUnauthorized)
				render.JSON(w, r, ErrUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		}
		return http.HandlerFunc(fn)
	}
}

func router(isDebug bool, secret string, cors Cors) *chi.Mux {
	r := chi.NewRouter()
	cors.Apply(r)
	if isDebug {
		r.Mount("/debug", func() http.Handler {
			r := chi.NewRouter()
			r.Put("/gc", func(w http.ResponseWriter, r *http.Request) {
				debug.FreeOSMemory()
			})
			handler := middleware.Profiler
			r.Mount("/", handler())
			return r
		}())
	}

	r.Group(func(r chi.Router) {
		if secret != "" {
			r.Use(authentication(secret))
		}
		r.Get("/", hello)
		r.Get("/get", getRandomProxy)
		r.Get("/all", getAllProxy)
		r.Post("/add", addProxy)
		r.Post("/delete", deleteProxy)
		r.Get("/port_map", getLocalPortMap)
	})

	return r
}

func getLocalPortMap(w http.ResponseWriter, r *http.Request) {
	resp := proxypool.GetLocalPortMap()
	render.Status(r, 200)
	render.JSON(w, r, resp)
}

func deleteProxy(w http.ResponseWriter, r *http.Request) {
	req := struct {
		Name string `json:"name"`
	}{}

	if err := render.DecodeJSON(r.Body, &req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrBadRequest)
		return
	}

	if err := proxypool.DeleteProxyByName(req.Name); err != nil {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, newError(err.Error()))
		return
	}

	render.Status(r, 200)
	render.JSON(w, r, map[string]string{"message": "ok"})
}

func addProxy(w http.ResponseWriter, r *http.Request) {
	req := proxypool.AddProxyReq{}
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrBadRequest)
		return
	}

	resp := &proxypool.AddProxyResp{}

	if req.SubUrl != "" {
		if err := proxypool.AddSubscriptionProxies(req, resp); err != nil {
			render.Status(r, http.StatusServiceUnavailable)
			render.JSON(w, r, newError(err.Error()))
			return
		}
	} else {
		if err := proxypool.AddProxy(req, resp); err != nil {
			render.Status(r, http.StatusServiceUnavailable)
			render.JSON(w, r, newError(err.Error()))
			return
		}
	}

	render.Status(r, 200)
	render.JSON(w, r, resp)
}

func getRandomProxy(w http.ResponseWriter, r *http.Request) {
	proxy, err := proxypool.GetRandomProxy()
	if err != nil {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, newError(err.Error()))
		return
	}
	render.JSON(w, r, proxy)
}

func convertIpRiskScore(percentage string) int {
	// 去掉百分号
	trimmed := strings.TrimSuffix(percentage, "%")

	// 将字符串转换为整数
	intValue, err := strconv.Atoi(trimmed)
	if err != nil {
		return 101
	}

	return intValue
}

func getAllProxy(w http.ResponseWriter, r *http.Request) {
	showConfig := r.URL.Query().Get("show_config") == "true"
	proxies, err := proxypool.GetAllProxies(showConfig)
	if err != nil {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, newError("Failed to retrieve proxies: "+err.Error()))
		return
	}

	sortProxies(proxies, r.URL.Query().Get("sort"))

	resp := map[string]any{
		"count":   len(proxies),
		"proxies": proxies,
	}

	render.JSON(w, r, resp)
}

func sortProxies(proxies []proxypool.ProxyResp, sortKey string) {
	switch sortKey {
	case "risk_score":
		sort.Slice(proxies, func(i, j int) bool {
			if proxies[i].IpRiskScore == "" {
				return false
			}

			si := convertIpRiskScore(proxies[i].IpRiskScore)
			sj := convertIpRiskScore(proxies[j].IpRiskScore)
			return si < sj
		})
	case "delay":
		sort.Slice(proxies, func(i, j int) bool {
			if proxies[i].Delay == 0 {
				return false
			}
			return proxies[i].Delay < proxies[j].Delay
		})
	case "time":
		sort.Slice(proxies, func(i, j int) bool {
			return proxies[i].AddTime.Unix() < proxies[j].AddTime.Unix()
		})
	}
}

func hello(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, render.M{"hello": "mihomo proxy pool"})
}

func Start(cfg *Config) error {
	// first stop existing server
	if httpServer != nil {
		_ = httpServer.Close()
		httpServer = nil
	}

	// handle addr
	if len(cfg.Addr) > 0 {
		l, err := inbound.Listen("tcp", cfg.Addr)
		if err != nil {
			log.Errorln("API serve listen error: %s", err)
			return err
		}
		log.Infoln("RESTful API listening at: %s", l.Addr().String())

		server := &http.Server{
			Handler: router(cfg.IsDebug, cfg.Secret, cfg.Cors),
		}
		httpServer = server
		if err = server.Serve(l); err != nil {
			log.Errorln("API serve error: %s", err)
			return err
		}
	}
	return nil
}
