package web

import (
	"context"
	"sync"

	"github.com/mordilloSan/go_logger/logger"
)

// RouteChannel represents a subscription to a specific route
type RouteChannel struct {
	Route  string
	ctx    context.Context
	cancel context.CancelFunc
}

// ChannelManager manages route subscriptions for a WebSocket connection
type ChannelManager struct {
	mu          sync.Mutex
	activeRoute string
	routes      map[string]*RouteChannel
	parentCtx   context.Context
}

// NewChannelManager creates a new channel manager
func NewChannelManager(parentCtx context.Context) *ChannelManager {
	return &ChannelManager{
		routes:    make(map[string]*RouteChannel),
		parentCtx: parentCtx,
	}
}

// Subscribe subscribes to a route, cancelling any previously active route
func (cm *ChannelManager) Subscribe(route string) context.Context {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Unsubscribe from previous route if switching routes
	if cm.activeRoute != "" && cm.activeRoute != route {
		if prevRoute, exists := cm.routes[cm.activeRoute]; exists {
			logger.Debugf("[ChannelManager] Cancelling route: %s", cm.activeRoute)
			prevRoute.cancel()
			delete(cm.routes, cm.activeRoute)
		}
	}

	// Check if route already exists (e.g., reconnecting to same route)
	if existing, exists := cm.routes[route]; exists {
		logger.Debugf("[ChannelManager] Already subscribed to route: %s", route)
		cm.activeRoute = route
		return existing.ctx
	}

	// Create new route subscription
	ctx, cancel := context.WithCancel(cm.parentCtx)
	rc := &RouteChannel{
		Route:  route,
		ctx:    ctx,
		cancel: cancel,
	}
	cm.routes[route] = rc
	cm.activeRoute = route

	logger.Debugf("[ChannelManager] Subscribed to route: %s", route)
	return ctx
}

// Unsubscribe unsubscribes from a route
func (cm *ChannelManager) Unsubscribe(route string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if rc, exists := cm.routes[route]; exists {
		logger.Debugf("[ChannelManager] Unsubscribing from route: %s", route)
		rc.cancel()
		delete(cm.routes, route)
	}

	if cm.activeRoute == route {
		cm.activeRoute = ""
	}
}

// GetActiveRoute returns the currently active route
func (cm *ChannelManager) GetActiveRoute() string {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.activeRoute
}

// CloseAll cancels all active routes
func (cm *ChannelManager) CloseAll() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for route, rc := range cm.routes {
		logger.Debugf("[ChannelManager] Closing route: %s", route)
		rc.cancel()
	}
	cm.routes = make(map[string]*RouteChannel)
	cm.activeRoute = ""
}
