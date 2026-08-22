package syncsvc

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// Scheduler 自动同步调度器：Start 即异步执行一轮启动同步（不阻塞启动），
// 之后按 interval 周期轮询；interval = 0 只跑启动轮。
//
// 同步失败不 fatal：错误经 SyncNow 记入 Last()（供状态接口读取），
// 调度器记日志后继续下一轮。生命周期与 gatepoll.Poller 同款：
// 一次构造，Start/Stop 控制后台循环，Stop 等在途一轮收口。
type Scheduler struct {
	svc      *Service
	interval time.Duration

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewScheduler 构造调度器。interval < 0 视为配置错误直接拒绝；
// 0 = 关闭周期轮询（启动同步仍执行）。
func NewScheduler(svc *Service, interval time.Duration) *Scheduler {
	return &Scheduler{svc: svc, interval: interval}
}

// Start 启动后台循环：先立即异步执行一轮启动同步，再按间隔周期执行。
// 循环随 ctx 取消或 Stop 结束。重复启动报错。Start 立即返回——启动同步
// 不阻塞调用方（服务启动路径）。
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return errors.New("syncsvc: 调度器已在运行")
	}
	if s.interval < 0 {
		return errors.New("syncsvc: 调度间隔不能为负")
	}
	ctx, s.cancel = context.WithCancel(ctx)
	done := make(chan struct{})
	s.done = done
	s.running = true
	go func() {
		defer close(done)
		s.loop(ctx)
	}()
	return nil
}

// Stop 优雅停止：取消循环并等待在途一轮结束。幂等；未启动时为 no-op。
func (s *Scheduler) Stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	stopped := !s.running
	if s.running {
		s.running = false
	}
	s.mu.Unlock()
	if stopped || cancel == nil {
		return
	}
	cancel()
	<-done
}

func (s *Scheduler) loop(ctx context.Context) {
	// 启动即先跑一轮：不等首个 tick。
	if _, err := s.svc.SyncNow(ctx); err != nil {
		log.Printf("task sync: 启动同步: %v", err)
	}
	if s.interval == 0 {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.svc.SyncNow(ctx); err != nil {
				log.Printf("task sync: 周期同步: %v", err)
			}
		}
	}
}
