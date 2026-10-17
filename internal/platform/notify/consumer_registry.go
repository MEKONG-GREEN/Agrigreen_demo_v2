package notify

import (
	"regexp"
	"slices"
	"sort"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
)

var consumerKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,159}$`)

type orderedConsumer struct {
	order    int
	consumer Consumer
}

type consumerRegistry struct{ specs []orderedConsumer }

func cloneConsumer(c Consumer) Consumer {
	c.Events = slices.Clone(c.Events)
	return c
}

// The base includes temporary legacy subscriptions during the two-step migration. Explicit owners
// may replace those entries, but cannot replace capability-owned subscriptions or their handlers.
func newConsumerRegistry(base []Consumer, nativeNames map[string]bool, registrations []port.ConsumerRegistration) *consumerRegistry {
	result := map[string]orderedConsumer{}
	nativeOrders, nativeEvents := map[int]bool{}, map[string]bool{}
	for i, c := range base {
		if _, ok := result[c.Name]; ok {
			panic("notify: duplicate base consumer " + c.Name)
		}
		result[c.Name] = orderedConsumer{order: i + 1, consumer: cloneConsumer(c)}
		if nativeNames[c.Name] {
			nativeOrders[i+1] = true
			for _, e := range c.Events {
				nativeEvents[e] = true
			}
		}
	}
	seen := map[string]bool{}
	for _, registration := range registrations {
		for _, d := range registration.Definitions {
			if nativeNames[d.Name] || nativeOrders[d.Order] || seen[d.Name] {
				panic("notify: duplicate or reserved consumer " + d.Name)
			}
			for _, e := range d.Events {
				if nativeEvents[e] {
					panic("notify: reserved consumer event " + e)
				}
			}
			seen[d.Name] = true
			result[d.Name] = orderedConsumer{order: d.Order, consumer: Consumer{Name: d.Name, Events: slices.Clone(d.Events)}}
		}
	}
	r := &consumerRegistry{}
	orders, events := map[int]bool{}, map[string]bool{}
	for _, sp := range result {
		c := sp.consumer
		if !consumerKeyPattern.MatchString(c.Name) || sp.order < 1 || sp.order > 65535 || orders[sp.order] || len(c.Events) == 0 {
			panic("notify: invalid consumer " + c.Name)
		}
		orders[sp.order] = true
		for _, e := range c.Events {
			if !consumerKeyPattern.MatchString(e) || events[e] {
				panic("notify: invalid or duplicate consumer event " + e)
			}
			events[e] = true
		}
		r.specs = append(r.specs, sp)
	}
	sort.Slice(r.specs, func(i, j int) bool { return r.specs[i].order < r.specs[j].order })
	return r
}

func (r *consumerRegistry) consumers(source inbox.Handler) []Consumer {
	out := make([]Consumer, len(r.specs))
	for i, sp := range r.specs {
		out[i] = cloneConsumer(sp.consumer)
		if out[i].Handler == nil {
			out[i].Handler = source
		}
	}
	return out
}
