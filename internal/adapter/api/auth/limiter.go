package auth

import (
	"net"
	"sync"
	"time"
)

const (
	baseLock      = time.Minute
	maxLock       = time.Hour
	maxTracked    = 100000
	idleForget    = 2 * time.Hour
	sweepInterval = time.Minute
)

type attempt struct {
	count       int
	windowStart time.Time
	lockUntil   time.Time
	level       int
	lastSeen    time.Time
}

// attemptLimiter cuenta fallos por clave (IP o usuario) en una ventana; al llegar al límite bloquea
// 1 min y duplica el bloqueo con cada reincidencia (tope 1 h). Los éxitos no cuentan.
type attemptLimiter struct {
	mu        sync.Mutex
	m         map[string]*attempt
	limit     int
	window    time.Duration
	now       func() time.Time
	lastSweep time.Time
	max       int // claves seguidas como máximo (maxTracked por defecto)
}

func newAttemptLimiter(limit int, window time.Duration, now func() time.Time) *attemptLimiter {
	return &attemptLimiter{m: map[string]*attempt{}, limit: limit, window: window, now: now, max: maxTracked}
}

// limiterIP agrupa las direcciones IPv6 por su prefijo /64 (lo que recibe una sola conexión doméstica o un
// servidor): quien tiene un /64 dispone de 2^64 direcciones y, contando cada una por separado, el límite por
// IP no limitaría nada.
func limiterIP(ip string) string {
	p := net.ParseIP(ip)
	if p == nil || p.To4() != nil {
		return ip
	}
	return p.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// blocked devuelve el tiempo que falta de bloqueo (0, false si no está bloqueada).
func (l *attemptLimiter) blocked(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.m[key]
	if a == nil {
		return 0, false
	}
	if rem := a.lockUntil.Sub(l.now()); rem > 0 {
		return rem, true
	}
	return 0, false
}

func (l *attemptLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	a := l.m[key]
	if a == nil {
		if len(l.m) >= l.max && !l.evict(now) {
			return
		}
		a = &attempt{windowStart: now}
		l.m[key] = a
	}
	a.lastSeen = now
	if a.lockUntil.After(now) {
		return
	}
	if now.Sub(a.windowStart) > l.window {
		a.count, a.windowStart = 0, now
	}
	a.count++
	if a.count >= l.limit {
		d := baseLock << uint(a.level)
		if d > maxLock || d <= 0 {
			d = maxLock
		}
		if a.level < 30 {
			a.level++
		}
		a.count = 0
		a.lockUntil = now.Add(d)
	}
}

// evict libera un hueco quitando una clave que no esté bloqueada. Con la tabla llena, no seguir a las
// claves nuevas dejaría a quien la llena con nombres inventados adivinar sin límite la clave de un usuario
// real que aún no estaba en la tabla.
func (l *attemptLimiter) evict(now time.Time) bool {
	for k, a := range l.m {
		if !a.lockUntil.After(now) {
			delete(l.m, k)
			return true
		}
	}
	return false
}

func (l *attemptLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now
	for k, a := range l.m {
		if !a.lockUntil.After(now) && now.Sub(a.lastSeen) > idleForget {
			delete(l.m, k)
		}
	}
}

// rateLimiter: ventana fija de un minuto por clave (600 peticiones/min por token).
type rateLimiter struct {
	mu        sync.Mutex
	m         map[string]*rateWin
	limit     int
	now       func() time.Time
	lastSweep time.Time
}

type rateWin struct {
	start time.Time
	n     int
}

func newRateLimiter(limit int, now func() time.Time) *rateLimiter {
	return &rateLimiter{m: map[string]*rateWin{}, limit: limit, now: now}
}

// allow cuenta una petición; si se pasa del límite devuelve el tiempo que falta para la ventana siguiente.
func (r *rateLimiter) allow(key string) (bool, time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if now.Sub(r.lastSweep) > sweepInterval {
		r.lastSweep = now
		for k, w := range r.m {
			if now.Sub(w.start) >= time.Minute {
				delete(r.m, k)
			}
		}
	}
	w := r.m[key]
	if w == nil || now.Sub(w.start) >= time.Minute {
		if w == nil && len(r.m) >= maxTracked {
			return true, 0
		}
		w = &rateWin{start: now}
		r.m[key] = w
	}
	w.n++
	if w.n > r.limit {
		return false, w.start.Add(time.Minute).Sub(now)
	}
	return true, 0
}
