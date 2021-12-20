package main

import (
	"flag"
	"log"
	"net/http"
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
		Name:      "energy_total_limit",
		Help:      "Maximum energy to be supplied in this charging session",
	})
)

func main() {
	log.SetFlags(0)
	flag.Parse()

	udp, err := newUDP("172.21.10.102")
	if err != nil {
		log.Fatalln(err)
	}

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(":2112", nil); err != nil {
			log.Fatalln(err)
		}
	}()

	ticker := time.NewTicker(10 * time.Second)
	for ; true; <-ticker.C {
		cfg, err := udp.Config()
		if err != nil {
			log.Println(err)
			continue
		}

		sess, err := udp.Session()
		if err != nil {
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

		gauge(currentLimit, "hw").Set(float64(cfg.MaxCurrent) / 1000)
		gauge(currentLimit, "user").Set(float64(cfg.CurrentLimit) / 1000)

		// power
		power.Set(float64(sess.Power) / 1000)

		// energy
		whTotal.Set(float64(sess.Total) / 1000)
		whSession.Set(float64(sess.Energy) / 1000)
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
