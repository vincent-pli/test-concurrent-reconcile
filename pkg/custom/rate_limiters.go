package custom

import (
	"sync"
	"time"

	workqueue "k8s.io/client-go/util/workqueue"
)

type ItemFixedDurationRateLimiter struct {
	failuresLock sync.Mutex
	failures     map[interface{}]int

	fixedDelay time.Duration
}

func NewFixedDurationRateLimiter(fixedDelay time.Duration) workqueue.RateLimiter {
	return &ItemFixedDurationRateLimiter{
		failures:   map[interface{}]int{},
		fixedDelay: fixedDelay,
	}
}

func (r *ItemFixedDurationRateLimiter) When(item interface{}) time.Duration {
	r.failuresLock.Lock()
	defer r.failuresLock.Unlock()

	r.failures[item] = r.failures[item] + 1

	calculated := time.Duration(r.fixedDelay)

	return calculated
}

func (r *ItemFixedDurationRateLimiter) NumRequeues(item interface{}) int {
	r.failuresLock.Lock()
	defer r.failuresLock.Unlock()

	return r.failures[item]
}

func (r *ItemFixedDurationRateLimiter) Forget(item interface{}) {
	r.failuresLock.Lock()
	defer r.failuresLock.Unlock()

	delete(r.failures, item)
}
