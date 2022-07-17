package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/pkg/errors"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

const (
	webHostFlag           = "host"
	webPortFlag           = "port"
	apiURLFlag            = "api-url"
	configURLTemplateFlag = "config-url-template"
)

type Web struct {
	host              string
	port              int
	apiURL            string
	configURLTemplate string
	m                 map[string]string

	mux sync.Mutex

	ln net.Listener
}

func NewWeb(c *cli.Context) *Web {
	return &Web{
		host:              c.String(webHostFlag),
		port:              c.Int(webPortFlag),
		apiURL:            c.String(apiURLFlag),
		configURLTemplate: c.String(configURLTemplateFlag),

		m: map[string]string{},
	}
}

func RegisterWebFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   webHostFlag,
			Usage:  "listening host",
			Value:  "",
			EnvVar: "WEB_HOST",
		},
		cli.IntFlag{
			Name:   webPortFlag,
			Usage:  "http listening port",
			Value:  8080,
			EnvVar: "WEB_PORT",
		},
		cli.StringFlag{
			Name:   apiURLFlag,
			Usage:  "fetch url",
			Value:  "https://api.nordvpn.com/v1/servers/recommendations?filters[country_id]=153&limit=20",
			EnvVar: "API_URL",
		},
		cli.StringFlag{
			Name:   configURLTemplateFlag,
			Usage:  "config url template",
			Value:  "https://downloads.nordcdn.com/configs/files/ovpn_legacy/servers/{hostname}.udp1194.ovpn",
			EnvVar: "CONFIG_URL_TEMPLATE",
		},
	)
}

func (s *Web) getConfig(node string) ([]byte, error) {
	if node == "favicon.ico" {
		return nil, nil
	}
	s.mux.Lock()
	defer s.mux.Unlock()
	resp, err := http.Get(s.apiURL)
	if err != nil {
		return nil, err
	}
	body := resp.Body
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	l := []map[string]interface{}{}
	err = json.Unmarshal(data, &l)
	if err != nil {
		return nil, err
	}
	delete(s.m, node)
	hosts := []string{}
	for _, v := range s.m {
		hosts = append(hosts, v)
	}
	h := ""
	for _, v := range l {
		h = v["hostname"].(string)
		found := false
		for _, eh := range hosts {
			if h == eh {
				found = true
			}
		}
		if found {
			continue
		}
		s.m[node] = h
		break
	}
	log.Infof("set %v for %v", h, node)
	if h == "" {
		return nil, errors.New("failed to find available vpn hostname")
	}
	configUrl := strings.ReplaceAll(s.configURLTemplate, "{hostname}", h)
	resp, err = http.Get(configUrl)
	if err != nil {
		return nil, err
	}
	body = resp.Body
	defer body.Close()
	data, err = io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *Web) serveConfig(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")

	c, err := s.getConfig(path)
	if err != nil {
		log.WithError(err).Errorf("failed to serve config fo %v", path)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write(c)
}

func (s *Web) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "failed to listen to tcp connection")
	}
	s.ln = ln
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.serveConfig)
	log.Infof("serving Web at %v", addr)
	return http.Serve(s.ln, mux)
}

func (s *Web) Close() {
	log.Info("closing Web")
	defer func() {
		log.Info("Web closed")
	}()
	if s.ln != nil {
		s.ln.Close()
	}
}
