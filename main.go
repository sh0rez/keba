package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const Namespace = "keba"

var (
	voltage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "voltage",
		Help:      "Voltage of the 3 phases in volts",
	}, []string{"phase"})

	current = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "current",
		Help:      "Current of the 3 phases in ampere",
	}, []string{"phase"})
	currentLimit = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "current_limit",
		Help:      "Maximum amperes permitted",
	}, []string{"kind"})

	power = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "power",
		Help:      "Power draw in watts",
	})

	whTotal     F
	whSession   F
	energyTotal = promauto.NewCounterFunc(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "energy_total_wh",
		Help:      "Total energy supplied by the wallbox in Wh",
	}, whTotal.Get)
	energySession = promauto.NewCounterFunc(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "energy_session_wh",
		Help:      "Energy supplied by the wallbox during this charging session in Wh",
	}, whSession.Get)
	energySessionLimit = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "energy_session_limit",
		Help:      "Maximum energy to be supplied in this charging session",
	})

	status = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "status",
		Help:      "State of the charging station (Starting, NotReady, Ready, Charging, Error, AuthRejected)",
	})
	plugStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "plug_status",
		Help:      "Status of the plug (cable)",
	}, []string{"kind"})
)

var (
	udpTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "scrape",
		Name:      "total",
	})

	udpErrs = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "scrape",
		Name:      "errors",
	})
)

func main() {
	addr := flag.String("http", ":2112", "http address to bind to")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Println("Error: Requires exactly 1 argument")
		flag.Usage()
		os.Exit(1)
	}

	udp, err := newUDP(flag.Arg(0))
	if err != nil {
		log.Fatalln(err)
	}

	go metrics(udp)
	go history(udp)

	log.Printf("http: listening on %s", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatalln(err)
	}
}

func metrics(udp Client) {
	http.Handle("/metrics", promhttp.Handler())

	ticker := time.NewTicker(10 * time.Second)
	for ; true; <-ticker.C {
		udpTotal.Inc()
		cfg, err := udp.Config()
		if err == nil {
			// current limits
			gauge(currentLimit, "hw").Set(float64(cfg.MaxCurrent) / 1000)
			gauge(currentLimit, "user").Set(float64(cfg.CurrentLimit) / 1000)

			// device status
			status.Set(float64(cfg.State))

			// plug status
			gauge(plugStatus, "station").Set(btof(cfg.Plug&PlugStation != 0))
			gauge(plugStatus, "locked").Set(btof(cfg.Plug&PlugLocked != 0))
			gauge(plugStatus, "ev").Set(btof(cfg.Plug&PlugEV != 0))
		} else {
			udpErrs.Inc()
			log.Println(err)
		}

		udpTotal.Inc()
		sess, err := udp.Session()
		if err != nil {
			udpErrs.Inc()
			log.Println(err)
			continue
		}

		// voltages
		gauge(voltage, "1").Set(float64(sess.Voltage1))
		gauge(voltage, "2").Set(float64(sess.Voltage2))
		gauge(voltage, "3").Set(float64(sess.Voltage3))

		// currents
		gauge(current, "1").Set(float64(sess.Current1) / 1000)
		gauge(current, "2").Set(float64(sess.Current2) / 1000)
		gauge(current, "3").Set(float64(sess.Current3) / 1000)

		// power
		power.Set(float64(sess.Power) / 1000)

		// energy
		whTotal.Set(float64(sess.Total) / 10)
		whSession.Set(float64(sess.Energy) / 10)
	}
}

func history(udp Client) {
	var hist []Log
	var mu sync.RWMutex

	http.HandleFunc("/history", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		defer mu.RUnlock()

		data, err := json.Marshal(hist)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Write(data)
	})

	ticker := time.NewTicker(10 * time.Second)
	for ; true; <-ticker.C {
		h, err := udp.History()
		if err != nil {
			log.Println(err)
			continue
		}
		mu.Lock()
		hist = h
		mu.Unlock()
	}
}

func gauge(vec *prometheus.GaugeVec, lvs ...string) prometheus.Gauge {
	g, err := vec.GetMetricWithLabelValues(lvs...)
	if err != nil {
		panic(err)
	}
	return g
}

// F is a concurrency safe float
type F struct {
	val float64
	mu  sync.RWMutex
}

func (f *F) Set(v float64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.val = v
}

func (f *F) Get() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return f.val
}

func btof(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
