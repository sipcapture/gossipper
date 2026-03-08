package engine

import "sync"

type commandMessage struct {
	raw  string
	from string
}

type commandBroker struct {
	mu     sync.Mutex
	queues map[string][]commandMessage
}

func newCommandBroker() *commandBroker {
	return &commandBroker{
		queues: make(map[string][]commandMessage),
	}
}

func (b *commandBroker) key(callID, channel string) string {
	return callID + "\x00" + channel
}

func (b *commandBroker) enqueue(callID, channel string, msg commandMessage) {
	key := b.key(callID, channel)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.queues[key] = append(b.queues[key], msg)
}

func (b *commandBroker) dequeue(callID, channel, src string) (commandMessage, bool) {
	key := b.key(callID, channel)
	b.mu.Lock()
	defer b.mu.Unlock()
	queue := b.queues[key]
	for i, msg := range queue {
		if src != "" && msg.from != src {
			continue
		}
		b.queues[key] = append(queue[:i], queue[i+1:]...)
		return msg, true
	}
	return commandMessage{}, false
}

func (b *commandBroker) dequeueAny(channel, src string) (string, commandMessage, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	suffix := "\x00" + channel
	for key, queue := range b.queues {
		if len(suffix) > 0 && len(key) >= len(suffix) && key[len(key)-len(suffix):] != suffix {
			continue
		}
		for i, msg := range queue {
			if src != "" && msg.from != src {
				continue
			}
			b.queues[key] = append(queue[:i], queue[i+1:]...)
			callID := key[:len(key)-len(suffix)]
			return callID, msg, true
		}
	}
	return "", commandMessage{}, false
}
