package log

import (
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

var errVmoduleSyntax = errors.New("expect comma-separated list of filename=N")

var errTraceSyntax = errors.New("expect file.go:234")

type GlogHandler struct {
	origin Handler

	level     uint32
	override  uint32
	backtrace uint32

	patterns  []pattern
	siteCache map[uintptr]Lvl
	location  string
	lock      sync.RWMutex
}

func NewGlogHandler(h Handler) *GlogHandler {
	return &GlogHandler{
		origin: h,
	}
}

type pattern struct {
	pattern *regexp.Regexp
	level   Lvl
}

func (h *GlogHandler) Verbosity(level Lvl) {
	atomic.StoreUint32(&h.level, uint32(level))
}

func (h *GlogHandler) Vmodule(ruleset string) error {
	var filter []pattern
	for _, rule := range strings.Split(ruleset, ",") {

		if len(rule) == 0 {
			continue
		}

		parts := strings.Split(rule, "=")
		if len(parts) != 2 {
			return errVmoduleSyntax
		}
		parts[0] = strings.TrimSpace(parts[0])
		parts[1] = strings.TrimSpace(parts[1])
		if len(parts[0]) == 0 || len(parts[1]) == 0 {
			return errVmoduleSyntax
		}

		level, err := strconv.Atoi(parts[1])
		if err != nil {
			return errVmoduleSyntax
		}
		if level <= 0 {
			continue
		}

		matcher := ".*"
		for _, comp := range strings.Split(parts[0], "/") {
			if comp == "*" {
				matcher += "(/.*)?"
			} else if comp != "" {
				matcher += "/" + regexp.QuoteMeta(comp)
			}
		}
		if !strings.HasSuffix(parts[0], ".go") {
			matcher += "/[^/]+\\.go"
		}
		matcher = matcher + "$"

		re, _ := regexp.Compile(matcher)
		filter = append(filter, pattern{re, Lvl(level)})
	}

	h.lock.Lock()
	defer h.lock.Unlock()

	h.patterns = filter
	h.siteCache = make(map[uintptr]Lvl)
	atomic.StoreUint32(&h.override, uint32(len(filter)))

	return nil
}

func (h *GlogHandler) BacktraceAt(location string) error {

	parts := strings.Split(location, ":")
	if len(parts) != 2 {
		return errTraceSyntax
	}
	parts[0] = strings.TrimSpace(parts[0])
	parts[1] = strings.TrimSpace(parts[1])
	if len(parts[0]) == 0 || len(parts[1]) == 0 {
		return errTraceSyntax
	}

	if !strings.HasSuffix(parts[0], ".go") {
		return errTraceSyntax
	}
	if _, err := strconv.Atoi(parts[1]); err != nil {
		return errTraceSyntax
	}

	h.lock.Lock()
	defer h.lock.Unlock()

	h.location = location
	atomic.StoreUint32(&h.backtrace, uint32(len(location)))

	return nil
}

func (h *GlogHandler) Log(r *Record) error {

	if atomic.LoadUint32(&h.backtrace) > 0 {

		h.lock.RLock()
		match := h.location == r.Call.String()
		h.lock.RUnlock()

		if match {

			r.Lvl = LvlInfo

			buf := make([]byte, 1024*1024)
			buf = buf[:runtime.Stack(buf, true)]
			r.Msg += "\n\n" + string(buf)
		}
	}

	if atomic.LoadUint32(&h.level) >= uint32(r.Lvl) {
		return h.origin.Log(r)
	}

	if atomic.LoadUint32(&h.override) == 0 {
		return nil
	}

	h.lock.RLock()
	lvl, ok := h.siteCache[r.Call.PC()]
	h.lock.RUnlock()

	if !ok {
		h.lock.Lock()
		for _, rule := range h.patterns {
			if rule.pattern.MatchString(fmt.Sprintf("%+s", r.Call)) {
				h.siteCache[r.Call.PC()], lvl, ok = rule.level, rule.level, true
				break
			}
		}

		if !ok {
			h.siteCache[r.Call.PC()] = 0
		}
		h.lock.Unlock()
	}
	if lvl >= r.Lvl {
		return h.origin.Log(r)
	}
	return nil
}
