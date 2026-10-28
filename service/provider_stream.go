package service

import (
	"crynux_as/config"
	"sync"
)

// ProviderStreamEvent is one normalized public stream event for a Provider job.
type ProviderStreamEvent struct {
	Kind    string
	Payload []byte
	Terminal bool
	Error   bool
}

type providerStreamSubscriber struct {
	ch     chan ProviderStreamEvent
	closed bool
}

// ProviderStreamRegistry is the process-local live stream fan-out keyed by task job ID.
type ProviderStreamRegistry struct {
	mu          sync.Mutex
	subscribers map[uint]*providerStreamSubscriber
	bufferSize  int
}

func NewProviderStreamRegistry(bufferSize int) *ProviderStreamRegistry {
	return &ProviderStreamRegistry{
		subscribers: make(map[uint]*providerStreamSubscriber),
		bufferSize:  bufferSize,
	}
}

var (
	providerStreams     *ProviderStreamRegistry
	providerStreamsOnce sync.Once
)

func InitProviderStreamRegistry(bufferSize int) {
	providerStreamsOnce.Do(func() {
		providerStreams = NewProviderStreamRegistry(bufferSize)
	})
}

func GetProviderStreamRegistry() *ProviderStreamRegistry {
	if providerStreams == nil {
		InitProviderStreamRegistry(int(config.GetConfig().Provider.StreamSubscriberBuffer))
	}
	return providerStreams
}

// Register creates a bounded subscriber for jobID. It must be called before the job becomes visible to the scheduler.
func (r *ProviderStreamRegistry) Register(jobID uint) <-chan ProviderStreamEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.subscribers[jobID]; ok {
		return existing.ch
	}
	sub := &providerStreamSubscriber{
		ch: make(chan ProviderStreamEvent, r.bufferSize),
	}
	r.subscribers[jobID] = sub
	return sub.ch
}

// Unregister removes and closes the subscriber for jobID.
func (r *ProviderStreamRegistry) Unregister(jobID uint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub, ok := r.subscribers[jobID]
	if !ok {
		return
	}
	delete(r.subscribers, jobID)
	if !sub.closed {
		sub.closed = true
		close(sub.ch)
	}
}

// Publish sends an event without blocking upstream reading. A full buffer detaches the subscriber.
func (r *ProviderStreamRegistry) Publish(jobID uint, event ProviderStreamEvent) {
	r.mu.Lock()
	sub, ok := r.subscribers[jobID]
	if !ok || sub.closed {
		r.mu.Unlock()
		return
	}
	select {
	case sub.ch <- event:
		r.mu.Unlock()
	default:
		delete(r.subscribers, jobID)
		sub.closed = true
		close(sub.ch)
		r.mu.Unlock()
	}
}

// Detach removes the subscriber without cancelling upstream execution.
func (r *ProviderStreamRegistry) Detach(jobID uint) {
	r.Unregister(jobID)
}
