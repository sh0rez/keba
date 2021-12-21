package main

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	StateStarting = iota
	StateNotReady
	StateReady
	StateCharging
	StateError
	StateAuthRejected
)

const (
	PlugStation = 0b001
	PlugLocked  = 0b010
	PlugEV      = 0b100
)

const (
	ReasonUnplug = 1
	ReasonRFID   = 10
)

const (
	dip1 = 1 << 16
	dip2 = 1 << 8

	DIP_Modbus = dip1 >> 2
	DIP_UDP    = dip1 >> 3
)

type System struct {
	Product  string `json:"Product"`
	Serial   string `json:"Serial"`
	Firmware string `json:"Firmware"`

	COMModule int `json:"COM-module"`
	Backend   int `json:"Backend"`

	DIPs DIPs `json:"DIP-Sw"`
}

type Config struct {
	State int `json:"State"`
	Plug  int `json:"Plug"`

	// max current the hardware can handle (dip setting, car, cable, temperature reduction)
	MaxCurrent int `json:"Curr HW"`

	// limits set by user
	CurrentLimit int `json:"Curr user"`
	EnergyLimit  int `json:"Setenergy"`

	// system uptime
	Seconds int `json:"Sec"`
}

type Session struct {
	Energy int `json:"E pres"`  // 0.1Wh
	Total  int `json:"E total"` // 0.1Wh

	// volts
	Voltage1 int `json:"U1"`
	Voltage2 int `json:"U2"`
	Voltage3 int `json:"U3"`

	// mA
	Current1 int `json:"I1"`
	Current2 int `json:"I2"`
	Current3 int `json:"I3"`

	// mW
	Power int `json:"P"`
}

type Log struct {
	Session int

	MaxCurrent int

	// Energies before and during the session
	StartTotal int
	Energy     int

	// Start and End seconds of the internal clock
	Start int
	End   int

	// Reason for the session to end
	EndReason int

	// RFID auth info
	RFIDTag   string
	RFIDClass string
}

// udpLog is like Log, but holds the respective JSON tags for unmarshalling
type udpLog struct {
	Session int `json:"Session ID"`

	MaxCurrent int `json:"Curr HW"`

	StartTotal int `json:"E Start"`
	Energy     int `json:"E Pres"`

	Start int
	End   int

	EndReason int `json:"reason"`

	RFIDTag   string `json:"RFID tag"`
	RFIDClass string `json:"RFID class"`
}

func (l *udpLog) UnmarshalJSON(data []byte) error {
	type T *udpLog
	if err := json.Unmarshal(data, T(l)); err != nil {
		return err
	}

	var times struct {
		Start json.Number `json:"started[s]"`
		End   json.Number `json:"ended[s]"`
	}
	if err := json.Unmarshal(data, &times); err != nil {
		return err
	}

	if i, err := times.Start.Int64(); err == nil {
		l.Start = int(i)
	}
	if i, err := times.End.Int64(); err == nil {
		l.End = int(i)
	}
	return nil
}

const Port = 7090

type Client interface {
	// System status
	System() (*System, error)

	// Energy config
	Config() (*Config, error)

	// Current session
	Session() (*Session, error)

	// History of charging sessions
	History() ([]Log, error)
}

type udp struct {
	addr net.UDPAddr
	mu   sync.Mutex
}

var _ Client = &udp{}

func newUDP(host string) (*udp, error) {
	var udp udp
	udp.addr.Port = Port

	if ip := net.ParseIP(host); ip != nil {
		udp.addr.IP = ip
		return &udp, nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no dns records found for '%s'", host)
	}
	udp.addr.IP = ips[0]

	return &udp, nil
}

func (u *udp) msg(cmd string, ptr interface{}) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	raddr := &u.addr
	laddr := &net.UDPAddr{Port: raddr.Port}
	laddr.IP = nil

	conn, err := net.DialUDP("udp", laddr, raddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(cmd)); err != nil {
		return err
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	return json.NewDecoder(conn).Decode(&ptr)
}

func (u *udp) System() (*System, error) {
	var s System
	err := u.msg("report 1", &s)
	return &s, err
}

func (u *udp) Config() (*Config, error) {
	var c Config
	err := u.msg("report 2", &c)
	return &c, err
}

func (u *udp) Session() (*Session, error) {
	var s Session
	err := u.msg("report 3", &s)
	return &s, err
}

func (u *udp) History() ([]Log, error) {
	hist := make([]Log, 0, 30)
	for i := 100; i <= 130; i++ {
		var l udpLog
		cmd := fmt.Sprintf("report %d", i)
		if err := u.msg(cmd, &l); err != nil {
			return nil, err
		}

		if l.Session < 0 {
			break
		}
		if l.Session == 0 {
			continue
		}
		if len(hist) > 0 && l.Session == hist[len(hist)-1].Session {
			continue
		}

		hist = append(hist, Log(l))
	}

	return hist, nil
}

type DIPs uint16

func (d DIPs) Has(i uint16) bool {
	return uint16(d)&i != 0
}

func (d *DIPs) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	i, err := strconv.ParseInt(strings.TrimPrefix(s, "0x"), 16, 16)
	if err != nil {
		return err
	}

	*d = DIPs(i)
	return nil
}

func (d *DIPs) MarshalJSON() ([]byte, error) {
	return []byte(d.String()), nil
}

func (d *DIPs) String() string {
	return fmt.Sprintf("%016b", *d)
}
