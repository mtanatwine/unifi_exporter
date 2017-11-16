// Command unifi_exporter provides a Prometheus exporter for a Ubiquiti UniFi
// Controller API and UniFi devices.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"strconv"
	"io/ioutil"
	"gopkg.in/yaml.v2"
	"github.com/mdlayher/unifi"
	"github.com/mdlayher/unifi_exporter"
	"github.com/prometheus/client_golang/prometheus"
)

type Config struct {
	Listen map[string]string `yaml:"listen"`
	Unifi map[string]string `yaml:"unifi"`
}

const (
	// userAgent is ther user agent reported to the UniFi Controller API.
	userAgent = "github.com/mdlayher/unifi_exporter"
)

func main() {
	var configFile = flag.String("config.file", "", "Relative path to config file yaml")
	flag.Parse()

	var config Config
	source, err := ioutil.ReadFile(*configFile)
	if err != nil {
		panic(err)
	}
	err = yaml.Unmarshal(source, &config)
	if err != nil {
		panic(err)
	}

	listenAddr    := config.Listen["address"] + ":" + config.Listen["port"]
	metricsPath   := config.Listen["metricspath"]
	unifiAddr     := config.Unifi["address"] + ":" + config.Unifi["port"]
	username      := config.Unifi["username"]
	password      := config.Unifi["password"]
	site          := config.Unifi["site"]
	b, err        := strconv.ParseBool(config.Unifi["insecure"])
	insecure      := b
	t, err        := strconv.Atoi(config.Unifi["timeoutsecs"])
	timeoutSecs   := time.Duration(t) * time.Second

	if unifiAddr == ":" {
		log.Fatal("address of UniFi Controller API must be specified within config file: ", *configFile)
	}
	if username == "" {
		log.Fatal("username to authenticate to UniFi Controller API must be specified within config file: ", *configFile)
	}
	if password == "" {
		log.Fatal("password to authenticate to UniFi Controller API must be specified within config file: ", *configFile)
	}
	if config.Listen["port"] == "" {
		// Set default port to 9130 if left blank in config.yml
		listenAddr = listenAddr + "9130"
	}

	clientFn := newClient(
		unifiAddr,
		username,
		password,
		insecure,
		timeoutSecs,
	)
	c, err := clientFn()
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}

	sites, err := c.Sites()
	if err != nil {
		log.Fatalf("failed to retrieve list of sites: %v", err)
	}

	useSites, err := pickSites(site, sites)
	if err != nil {
		log.Fatalf("failed to select a site: %v", err)
	}

	e, err := unifiexporter.New(useSites, clientFn)
	if err != nil {
		log.Fatalf("failed to create exporter: %v", err)
	}

	prometheus.MustRegister(e)

	http.Handle(metricsPath, prometheus.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, metricsPath, http.StatusMovedPermanently)
	})

	log.Printf("Starting UniFi exporter on %q for site(s): %s", listenAddr, sitesString(useSites))

	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		log.Fatalf("cannot start UniFi exporter: %s", err)
	}
}

// pickSites attempts to find a site with a description matching the value
// specified in choose.  If choose is empty, all sites are returned.
func pickSites(choose string, sites []*unifi.Site) ([]*unifi.Site, error) {
	if choose == "" {
		return sites, nil
	}

	var pick *unifi.Site
	for _, s := range sites {
		if s.Description == choose {
			pick = s
			break
		}
	}
	if pick == nil {
		return nil, fmt.Errorf("site with description %q was not found in UniFi Controller", choose)
	}

	return []*unifi.Site{pick}, nil
}

// sitesString returns a comma-separated string of site descriptions, meant
// for displaying to users.
func sitesString(sites []*unifi.Site) string {
	ds := make([]string, 0, len(sites))
	for _, s := range sites {
		ds = append(ds, s.Description)
	}

	return strings.Join(ds, ", ")
}

// newClient returns a unifiexporter.ClientFunc using the input parameters.
func newClient(addr, username, password string, insecure bool, timeoutSecs time.Duration) unifiexporter.ClientFunc {
	return func() (*unifi.Client, error) {
		httpClient := &http.Client{Timeout: timeoutSecs}
		if insecure {
			httpClient = unifi.InsecureHTTPClient(timeoutSecs)
		}

		c, err := unifi.NewClient(addr, httpClient)
		if err != nil {
			return nil, fmt.Errorf("cannot create UniFi Controller client: %v", err)
		}
		c.UserAgent = userAgent

		if err := c.Login(username, password); err != nil {
			return nil, fmt.Errorf("failed to authenticate to UniFi Controller: %v", err)
		}

		return c, nil
	}
}
