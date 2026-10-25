package service

import "errors"

var ErrQueueTimeoutZero = errors.New("queue_timeout must be greater than 0")

// ValidateOptionalQueueTimeout accepts a nil value or a positive second count.
func ValidateOptionalQueueTimeout(queueTimeout *uint64) error {
	if queueTimeout == nil {
		return nil
	}
	if *queueTimeout == 0 {
		return ErrQueueTimeoutZero
	}
	return nil
}
