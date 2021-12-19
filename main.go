package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/sanity-io/litter"
)

func main() {
	log.SetFlags(0)

	udp, err := newUDP("172.21.10.102")
	if err != nil {
		log.Fatalln(err)
	}

	var r1 Report1
	if err := udp.msg("report 1", &r1); err != nil {
		log.Fatalln(err)
	}

	litter.Dump(r1)
}

type Report1 struct {
	ID int `json:"ID,string"`

	Product  string `json:"Product"`
	Serial   string `json:"Serial"`
	Firmware string `json:"Firmware"`

	COMModule int `json:"COM-module"`
	Backend   int `json:"Backend"`

	DIPs DIPs `json:"DIP-Sw"`
}

const (
	dip1 = 1 << 16
	dip2 = 1 << 8

	DIP_Modbus = dip1 >> 2
	DIP_UDP    = dip1 >> 3
)

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
	return fmt.Sprintf("0x%x", uint16(*d))
}

type client interface {
	msg(cmd string, ptr interface{}) error
}

const port = 7090

func newUDP(addr string) (*udp, error) {
	var udp udp
	udp.addr.Port = port

	if ip := net.ParseIP(addr); ip != nil {
		udp.addr.IP = ip
		return &udp, nil
	}

	ips, err := net.LookupIP(addr)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no dns records found for '%s'", addr)
	}
	udp.addr.IP = ips[0]

	return &udp, nil
}

type udp struct {
	addr net.UDPAddr
}

func (u *udp) msg(cmd string, ptr interface{}) error {
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
