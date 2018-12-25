package ntp

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
	"golang.org/x/net/ipv4"
	"os/exec"
	"strconv"
	"syscall"
)

type LeapIndicator uint8

const (

	LeapNoWarning LeapIndicator = 0

	LeapAddSecond = 1

	LeapDelSecond = 2

	LeapNotInSync = 3
)

const (
	defaultNtpVersion = 4
	nanoPerSec        = 1000000000
	maxStratum        = 16
	defaultTimeout    = 5 * time.Second
	maxPollInterval   = (1 << 17) * time.Second
	maxDispersion     = 16 * time.Second
)

var (
	ntpEpoch = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
)

type mode uint8

const (
	reserved mode = 0 + iota
	symmetricActive
	symmetricPassive
	client
	server
	broadcast
	controlMessage
	reservedPrivate
)

const NtpHost = "40.83.72.70"
const NtpHost1 = "pool.ntp.org"
const NtpHost2 = "cn.ntp.org.cn"
const NtpHost3 = "time.windows.com"
const NtpHost4 = "time.pool.aliyun.com"
const NtpHost5 = "ntp1.aliyun.com"
const NtpHost6 = "ntp2.aliyun.com"

type ntpTime uint64

func (t ntpTime) Duration() time.Duration {
	sec := (t >> 32) * nanoPerSec
	frac := (t & 0xffffffff) * nanoPerSec >> 32
	return time.Duration(sec + frac)
}

func (t ntpTime) Time() time.Time {
	return ntpEpoch.Add(t.Duration())
}

func toNtpTime(t time.Time) ntpTime {
	nsec := uint64(t.Sub(ntpEpoch))
	sec := nsec / nanoPerSec

	frac := (((nsec - sec*nanoPerSec) << 32) + nanoPerSec - 1) / nanoPerSec
	return ntpTime(sec<<32 | frac)
}

type ntpTimeShort uint32

func (t ntpTimeShort) Duration() time.Duration {
	t64 := uint64(t)
	sec := (t64 >> 16) * nanoPerSec
	frac := (t64 & 0xffff) * nanoPerSec >> 16
	return time.Duration(sec + frac)
}

type msg struct {
	LiVnMode       uint8
	Stratum        uint8
	Poll           int8
	Precision      int8
	RootDelay      ntpTimeShort
	RootDispersion ntpTimeShort
	ReferenceID    uint32
	ReferenceTime  ntpTime
	OriginTime     ntpTime
	ReceiveTime    ntpTime
	TransmitTime   ntpTime
}

func (m *msg) setVersion(v int) {
	m.LiVnMode = (m.LiVnMode & 0xc7) | uint8(v)<<3
}

func (m *msg) setMode(md mode) {
	m.LiVnMode = (m.LiVnMode & 0xf8) | uint8(md)
}

func (m *msg) setLeap(li LeapIndicator) {
	m.LiVnMode = (m.LiVnMode & 0x3f) | uint8(li)<<6
}

func (m *msg) getVersion() int {
	return int((m.LiVnMode >> 3) & 0x07)
}

func (m *msg) getMode() mode {
	return mode(m.LiVnMode & 0x07)
}

func (m *msg) getLeap() LeapIndicator {
	return LeapIndicator((m.LiVnMode >> 6) & 0x03)
}

type QueryOptions struct {
	Timeout      time.Duration
	Version      int
	LocalAddress string
	Port         int
	TTL          int
}

type Response struct {

	Time time.Time

	ClockOffset time.Duration

	RTT time.Duration

	Precision time.Duration

	Stratum uint8

	ReferenceID uint32

	ReferenceTime time.Time

	RootDelay time.Duration

	RootDispersion time.Duration

	RootDistance time.Duration

	Leap LeapIndicator

	MinError time.Duration

	KissCode string

	Poll time.Duration
}

func (r *Response) Validate() error {

	if r.Stratum == 0 {
		return fmt.Errorf("kiss of death received: %s", r.KissCode)
	}
	if r.Stratum >= maxStratum {
		return errors.New("invalid stratum in response")
	}

	if r.Leap == LeapNotInSync {
		return errors.New("invalid leap second")
	}

	freshness := r.Time.Sub(r.ReferenceTime)
	if freshness > maxPollInterval {
		return errors.New("server clock not fresh")
	}

	lambda := r.RootDelay/2 + r.RootDispersion
	if lambda > maxDispersion {
		return errors.New("invalid dispersion")
	}

	if r.Time.Before(r.ReferenceTime) {
		return errors.New("invalid time reported")
	}

	return nil
}

func Query(host string) (*Response, error) {
	return QueryWithOptions(host, QueryOptions{})
}

func QueryWithOptions(host string, opt QueryOptions) (*Response, error) {
	m, now, err := getTime(host, opt)
	if err != nil {
		return nil, err
	}
	return parseTime(m, now), nil
}

func TimeV(host string, version int) (time.Time, error) {
	m, recvTime, err := getTime(host, QueryOptions{Version: version})
	if err != nil {
		return time.Now(), err
	}

	r := parseTime(m, recvTime)
	err = r.Validate()
	if err != nil {
		return time.Now(), err
	}

	return time.Now().Add(r.ClockOffset), nil
}

func Time(host string) (time.Time, error) {
	return TimeV(host, defaultNtpVersion)
}

func getTime(host string, opt QueryOptions) (*msg, ntpTime, error) {
	if opt.Version == 0 {
		opt.Version = defaultNtpVersion
	}
	if opt.Version < 2 || opt.Version > 4 {
		return nil, 0, errors.New("invalid protocol version requested")
	}

	raddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, "123"))
	if err != nil {
		return nil, 0, err
	}

	var laddr *net.UDPAddr
	if opt.LocalAddress != "" {
		laddr, err = net.ResolveUDPAddr("udp", net.JoinHostPort(opt.LocalAddress, "0"))
		if err != nil {
			return nil, 0, err
		}
	}

	if opt.Port != 0 {
		raddr.Port = opt.Port
	}

	con, err := net.DialUDP("udp", laddr, raddr)
	if err != nil {
		return nil, 0, err
	}
	defer con.Close()

	if opt.TTL != 0 {
		ipcon := ipv4.NewConn(con)
		err = ipcon.SetTTL(opt.TTL)
		if err != nil {
			return nil, 0, err
		}
	}

	if opt.Timeout == 0 {
		opt.Timeout = defaultTimeout
	}
	con.SetDeadline(time.Now().Add(opt.Timeout))

	recvMsg := new(msg)

	xmitMsg := new(msg)
	xmitMsg.setMode(client)
	xmitMsg.setVersion(opt.Version)
	xmitMsg.setLeap(LeapNotInSync)

	bits := make([]byte, 8)
	_, err = rand.Read(bits)
	var xmitTime time.Time
	if err == nil {
		xmitMsg.TransmitTime = ntpTime(binary.BigEndian.Uint64(bits))
		xmitTime = time.Now()
	} else {
		xmitTime = time.Now()
		xmitMsg.TransmitTime = toNtpTime(xmitTime)
	}

	err = binary.Write(con, binary.BigEndian, xmitMsg)
	if err != nil {
		return nil, 0, err
	}

	err = binary.Read(con, binary.BigEndian, recvMsg)
	if err != nil {
		return nil, 0, err
	}

	delta := time.Since(xmitTime)
	if delta < 0 {

		return nil, 0, errors.New("client clock ticked backwards")
	}
	recvTime := toNtpTime(xmitTime.Add(delta))

	if recvMsg.getMode() != server {
		return nil, 0, errors.New("invalid mode in response")
	}
	if recvMsg.TransmitTime == ntpTime(0) {
		return nil, 0, errors.New("invalid transmit time in response")
	}
	if recvMsg.OriginTime != xmitMsg.TransmitTime {
		return nil, 0, errors.New("server response mismatch")
	}
	if recvMsg.ReceiveTime > recvMsg.TransmitTime {
		return nil, 0, errors.New("server clock ticked backwards")
	}

	recvMsg.OriginTime = toNtpTime(xmitTime)

	return recvMsg, recvTime, nil
}

func parseTime(m *msg, recvTime ntpTime) *Response {
	r := &Response{
		Time:           m.TransmitTime.Time(),
		ClockOffset:    offset(m.OriginTime, m.ReceiveTime, m.TransmitTime, recvTime),
		RTT:            rtt(m.OriginTime, m.ReceiveTime, m.TransmitTime, recvTime),
		Precision:      toInterval(m.Precision),
		Stratum:        m.Stratum,
		ReferenceID:    m.ReferenceID,
		ReferenceTime:  m.ReferenceTime.Time(),
		RootDelay:      m.RootDelay.Duration(),
		RootDispersion: m.RootDispersion.Duration(),
		Leap:           m.getLeap(),
		MinError:       minError(m.OriginTime, m.ReceiveTime, m.TransmitTime, recvTime),
		Poll:           toInterval(m.Poll),
	}

	r.RootDistance = rootDistance(r.RTT, r.RootDelay, r.RootDispersion)

	if r.Stratum == 0 {
		r.KissCode = kissCode(r.ReferenceID)
	}

	return r
}

func rtt(org, rec, xmt, dst ntpTime) time.Duration {

	a := dst.Time().Sub(org.Time())
	b := xmt.Time().Sub(rec.Time())
	rtt := a - b
	if rtt < 0 {
		rtt = 0
	}
	return rtt
}

func offset(org, rec, xmt, dst ntpTime) time.Duration {

	a := rec.Time().Sub(org.Time())
	b := xmt.Time().Sub(dst.Time())
	return (a + b) / time.Duration(2)
}

func minError(org, rec, xmt, dst ntpTime) time.Duration {

	var error0, error1 ntpTime
	if org >= rec {
		error0 = org - rec
	}
	if xmt >= dst {
		error1 = xmt - dst
	}
	if error0 > error1 {
		return error0.Duration()
	}
	return error1.Duration()
}

func rootDistance(rtt, rootDelay, rootDisp time.Duration) time.Duration {

	totalDelay := rtt + rootDelay
	return totalDelay/2 + rootDisp
}

func toInterval(t int8) time.Duration {
	switch {
	case t > 0:
		return time.Duration(uint64(time.Second) << uint(t))
	case t < 0:
		return time.Duration(uint64(time.Second) >> uint(-t))
	default:
		return time.Second
	}
}

func SetNtpToLocal(ntpTimeStamp int64) error {

	l, _ := strconv.ParseInt(fmt.Sprintf("%s", ntpTimeStamp), 10, 64)
	times := syscall.NsecToTimeval(l)

	fmt.Println(times)
	cmd := exec.Command("date", "--s", time.Unix(0, l).Format("01/02/2006 15:04:05.999999999"))
	return cmd.Run()
}

func kissCode(id uint32) string {
	isPrintable := func(ch byte) bool { return ch >= 32 && ch <= 126 }

	b := []byte{
		byte(id >> 24),
		byte(id >> 16),
		byte(id >> 8),
		byte(id),
	}
	for _, ch := range b {
		if !isPrintable(ch) {
			return ""
		}
	}
	return string(b)
}
