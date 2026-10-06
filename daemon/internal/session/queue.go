// Package session serializes commands per session: commands of one session run one at a time in
// arrival order, different sessions run in parallel.
package session

import (
	"context"
	"sync"
)

type Queues struct {
	mu    sync.Mutex
	m     map[string]*queue
	known map[string]struct{}
}

type queue struct {
	// turn has capacity 1; the goroutine whose send succeeded is the one running. Blocked senders
	// on a Go channel are woken in FIFO order, which gives arrival-order execution.
	turn  chan struct{}
	users int // running plus waiting; the entry is dropped when it reaches 0
}

func New() *Queues {
	return &Queues{m: map[string]*queue{}, known: map[string]struct{}{}}
}

// Acquire waits for the session's turn. If ctx ends first it returns ctx.Err(); otherwise the
// caller must call release exactly once.
func (q *Queues) Acquire(ctx context.Context, session string) (release func(), err error) {
	q.mu.Lock()
	s := q.m[session]
	if s == nil {
		s = &queue{turn: make(chan struct{}, 1)}
		q.m[session] = s
	}
	s.users++
	q.known[session] = struct{}{}
	q.mu.Unlock()

	select {
	case s.turn <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-s.turn
				q.leave(session, s)
			})
		}, nil
	case <-ctx.Done():
		q.leave(session, s)
		return nil, ctx.Err()
	}
}

func (q *Queues) leave(session string, s *queue) {
	q.mu.Lock()
	defer q.mu.Unlock()
	s.users--
	if s.users == 0 && q.m[session] == s {
		delete(q.m, session)
	}
}

// Forget drops a session from Count after close_session succeeded.
func (q *Queues) Forget(session string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.known, session)
}

// Count is the number of sessions that have sent a command and not been closed.
func (q *Queues) Count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.known)
}

func (q *Queues) users(session string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	if s := q.m[session]; s != nil {
		return s.users
	}
	return 0
}
