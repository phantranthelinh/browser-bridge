package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func waitUsers(t *testing.T, q *Queues, session string, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for q.users(session) != n {
		if time.Now().After(deadline) {
			t.Fatalf("session %s: want %d users, have %d", session, n, q.users(session))
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSameSessionRunsInArrivalOrder(t *testing.T) {
	q := New()
	ctx := context.Background()
	release, err := q.Acquire(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var order []int
	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := q.Acquire(ctx, "s")
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
			r()
		}()
		waitUsers(t, q, "s", i+1)
		// users is counted just before the goroutine blocks on the channel; give it time to block
		// so the next goroutine really arrives after it.
		time.Sleep(10 * time.Millisecond)
	}
	release()
	wg.Wait()
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("order = %v", order)
	}
}

func TestDifferentSessionsRunInParallel(t *testing.T) {
	q := New()
	ra, _ := q.Acquire(context.Background(), "a")
	defer ra()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	rb, err := q.Acquire(ctx, "b")
	if err != nil {
		t.Fatalf("session b blocked by session a: %v", err)
	}
	rb()
}

func TestWaitingPastDeadlineGivesUp(t *testing.T) {
	q := New()
	r, _ := q.Acquire(context.Background(), "s")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := q.Acquire(ctx, "s")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	r()
	waitUsers(t, q, "s", 0)
	// the abandoned waiter must not hold the turn
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	r2, err := q.Acquire(ctx2, "s")
	if err != nil {
		t.Fatalf("queue stuck after a waiter timed out: %v", err)
	}
	r2()
}

func TestReleaseTwiceIsHarmless(t *testing.T) {
	q := New()
	r, _ := q.Acquire(context.Background(), "s")
	r()
	r()
	if q.users("s") != 0 {
		t.Fatal("double release corrupted the queue")
	}
}

func TestCountAndForget(t *testing.T) {
	q := New()
	for _, s := range []string{"a", "b", "a"} {
		r, _ := q.Acquire(context.Background(), s)
		r()
	}
	if q.Count() != 2 {
		t.Fatalf("Count = %d", q.Count())
	}
	q.Forget("a")
	if q.Count() != 1 {
		t.Fatalf("Count after Forget = %d", q.Count())
	}
}
