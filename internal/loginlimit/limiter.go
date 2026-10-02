// Package loginlimit 给面板登录做失败次数限制，防止被在线爆破。
// 只作用于面板的 Web 登录，不涉及 SSH。
package loginlimit

import (
	"sync"
	"time"
)

type Limiter struct {
	mu     sync.Mutex
	items  map[string]*item
	max    int
	window time.Duration
	lock   time.Duration
	now    func() time.Time
}

type item struct {
	count int
	first time.Time
	until time.Time
}

func New(max int, window, lock time.Duration) *Limiter {
	if max <= 0 {
		max = 5
	}
	if window <= 0 {
		window = 15 * time.Minute
	}
	if lock <= 0 {
		lock = 15 * time.Minute
	}
	return &Limiter{
		items:  make(map[string]*item),
		max:    max,
		window: window,
		lock:   lock,
		now:    time.Now,
	}
}

// Allowed 判断是否放行；被锁定时返回还要等多久。
func (l *Limiter) Allowed(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	it, ok := l.items[key]
	if !ok {
		return true, 0
	}
	now := l.now()
	if now.Before(it.until) {
		return false, it.until.Sub(now)
	}
	if now.Sub(it.first) > l.window {
		delete(l.items, key)
	}
	return true, 0
}

// Fail 记一次失败，达到上限就锁定一段时间。
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	it := l.items[key]
	if it == nil || now.Sub(it.first) > l.window {
		it = &item{first: now}
		l.items[key] = it
	}
	it.count++
	if it.count >= l.max {
		it.until = now.Add(l.lock)
		it.count = 0
		it.first = now
	}
}

// Success 登录成功后清掉记录。
func (l *Limiter) Success(key string) {
	l.mu.Lock()
	delete(l.items, key)
	l.mu.Unlock()
}

// Cleanup 清掉过期条目，避免内存一直涨。
func (l *Limiter) Cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, it := range l.items {
		if now.After(it.until) && now.Sub(it.first) > l.window {
			delete(l.items, k)
		}
	}
}
