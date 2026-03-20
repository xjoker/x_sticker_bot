package ratelimit

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter 提供全局和按用户的速率限制。
type RateLimiter struct {
	global       *rate.Limiter
	users        sync.Map // map[int64]*userLimiter
	commandRate  map[string]rate.Limit
	commandBurst map[string]int
	defaultRate  rate.Limit
	defaultBurst int
	denied       atomic.Int64
}

type userLimiter struct {
	general  *rate.Limiter
	commands map[string]*rate.Limiter
	lastSeen time.Time
	mu       sync.Mutex
}

// New 创建一个新的 RateLimiter，配置全局速率限制。
func New(globalRate rate.Limit, globalBurst int) *RateLimiter {
	return &RateLimiter{
		global:       rate.NewLimiter(globalRate, globalBurst),
		commandRate:  make(map[string]rate.Limit),
		commandBurst: make(map[string]int),
		defaultRate:  rate.Limit(30.0 / 60.0), // 30 req/min
		defaultBurst: 5,
	}
}

// SetCommandRate 配置特定命令的速率限制。
func (rl *RateLimiter) SetCommandRate(command string, r rate.Limit, burst int) {
	rl.commandRate[command] = r
	rl.commandBurst[command] = burst
}

// getOrCreateUser 获取或创建用户的限制器。
func (rl *RateLimiter) getOrCreateUser(uid int64) *userLimiter {
	if v, ok := rl.users.Load(uid); ok {
		return v.(*userLimiter)
	}
	ul := &userLimiter{
		general:  rate.NewLimiter(rl.defaultRate, rl.defaultBurst),
		commands: make(map[string]*rate.Limiter),
		lastSeen: time.Now(),
	}
	actual, _ := rl.users.LoadOrStore(uid, ul)
	return actual.(*userLimiter)
}

// Allow 检查用户是否被允许执行一般操作。
func (rl *RateLimiter) Allow(uid int64) bool {
	ul := rl.getOrCreateUser(uid)
	ul.mu.Lock()
	ul.lastSeen = time.Now()
	ul.mu.Unlock()

	if !rl.global.Allow() {
		rl.denied.Add(1)
		slog.Debug("global rate limit exceeded", "uid", uid)
		return false
	}
	if !ul.general.Allow() {
		rl.denied.Add(1)
		slog.Debug("user rate limit exceeded", "uid", uid)
		return false
	}
	return true
}

// AllowCommand 检查用户是否被允许执行特定命令。
// 同时检查全局限制和用户的命令级限制。
func (rl *RateLimiter) AllowCommand(uid int64, command string) bool {
	ul := rl.getOrCreateUser(uid)
	ul.mu.Lock()
	ul.lastSeen = time.Now()

	cmdLimiter, ok := ul.commands[command]
	if !ok {
		r, rOk := rl.commandRate[command]
		b, bOk := rl.commandBurst[command]
		if !rOk || !bOk {
			// 没有配置命令级限制，使用默认值
			r = rl.defaultRate
			b = rl.defaultBurst
		}
		cmdLimiter = rate.NewLimiter(r, b)
		ul.commands[command] = cmdLimiter
	}
	ul.mu.Unlock()

	if !rl.global.Allow() {
		rl.denied.Add(1)
		slog.Debug("global rate limit exceeded for command", "uid", uid, "command", command)
		return false
	}
	if !cmdLimiter.Allow() {
		rl.denied.Add(1)
		slog.Debug("command rate limit exceeded", "uid", uid, "command", command)
		return false
	}
	return true
}

// CleanupStale 删除空闲时间超过 maxIdle 的用户限制器。
func (rl *RateLimiter) CleanupStale(maxIdle time.Duration) {
	now := time.Now()
	var cleaned int
	rl.users.Range(func(key, value any) bool {
		ul := value.(*userLimiter)
		ul.mu.Lock()
		idle := now.Sub(ul.lastSeen) > maxIdle
		ul.mu.Unlock()
		if idle {
			rl.users.Delete(key)
			cleaned++
		}
		return true
	})
	if cleaned > 0 {
		slog.Debug("cleaned stale user limiters", "count", cleaned)
	}
}

// StartCleanup 启动后台清理协程，定期清理过期的用户限制器。
func (rl *RateLimiter) StartCleanup(interval, maxIdle time.Duration, stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rl.CleanupStale(maxIdle)
			case <-stop:
				return
			}
		}
	}()
}

