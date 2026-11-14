// Package notify provides actionable desktop notifications over the
// org.freedesktop.Notifications D-Bus interface.
package notify

import (
	"errors"
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	service   = "org.freedesktop.Notifications"
	iface     = service
	object    = dbus.ObjectPath("/org/freedesktop/Notifications")
	eventBuf  = 16
	expireSet = int32(-1)
)

// Conn is the notification connection used by the UI. Implementations are
// safe to call from Bubble Tea commands while Events is being consumed.
type Conn interface {
	Notify(appName, icon, summary, body string, actions []string) (uint32, error)
	CloseNotification(id uint32) error
	Events() <-chan Event
	Close() error
}

// Event is an actionable-notification signal. Closed events carry no action.
type Event struct {
	ID        uint32
	ActionKey string
	Closed    bool
}

type transport interface {
	Capabilities() ([]string, error)
	Notify(appName, icon, summary, body string, actions []string) (uint32, error)
	CloseNotification(id uint32) error
	Signals() <-chan *dbus.Signal
	Close() error
}

type connection struct {
	appName   string
	transport transport
	events    chan Event
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// Open creates a private session-bus connection and verifies that the active
// notification daemon supports actions. The caller owns the returned Conn.
func Open(appName string) (Conn, error) {
	t, err := openDBusTransport()
	if err != nil {
		return nil, err
	}
	return newConnection(appName, t)
}

func newConnection(appName string, t transport) (Conn, error) {
	capabilities, err := t.Capabilities()
	if err != nil {
		_ = t.Close()
		return nil, fmt.Errorf("notify: get capabilities: %w", err)
	}
	actions := false
	for _, capability := range capabilities {
		if capability == "actions" {
			actions = true
			break
		}
	}
	if !actions {
		_ = t.Close()
		return nil, errors.New("notify: notification daemon does not support actions")
	}

	c := &connection{
		appName:   appName,
		transport: t,
		events:    make(chan Event, eventBuf),
		done:      make(chan struct{}),
	}
	go c.forwardSignals()
	return c, nil
}

func (c *connection) Notify(appName, icon, summary, body string, actions []string) (uint32, error) {
	if appName == "" {
		appName = c.appName
	}
	return c.transport.Notify(appName, icon, summary, body, actions)
}

func (c *connection) CloseNotification(id uint32) error {
	return c.transport.CloseNotification(id)
}

func (c *connection) Events() <-chan Event { return c.events }

func (c *connection) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.closeErr = c.transport.Close()
	})
	return c.closeErr
}

func (c *connection) forwardSignals() {
	defer close(c.events)
	for signal := range c.transport.Signals() {
		if event, ok := decodeSignal(signal); ok {
			select {
			case c.events <- event:
			case <-c.done:
				return
			}
		}
	}
}

func decodeSignal(signal *dbus.Signal) (Event, bool) {
	if signal == nil || len(signal.Body) < 2 {
		return Event{}, false
	}
	id, ok := signal.Body[0].(uint32)
	if !ok || id == 0 {
		return Event{}, false
	}
	switch signal.Name {
	case iface + ".ActionInvoked":
		action, ok := signal.Body[1].(string)
		if !ok || action == "" {
			return Event{}, false
		}
		return Event{ID: id, ActionKey: action}, true
	case iface + ".NotificationClosed":
		if _, ok := signal.Body[1].(uint32); !ok {
			return Event{}, false
		}
		return Event{ID: id, Closed: true}, true
	default:
		return Event{}, false
	}
}

type dbusTransport struct {
	conn    *dbus.Conn
	object  dbus.BusObject
	signals chan *dbus.Signal
	match   []dbus.MatchOption
}

func openDBusTransport() (*dbusTransport, error) {
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		return nil, fmt.Errorf("notify: connect session bus: %w", err)
	}
	fail := func(step string, cause error) (*dbusTransport, error) {
		_ = conn.Close()
		return nil, fmt.Errorf("notify: %s: %w", step, cause)
	}
	if err := conn.Auth(nil); err != nil {
		return fail("authenticate session bus", err)
	}
	if err := conn.Hello(); err != nil {
		return fail("hello session bus", err)
	}

	match := []dbus.MatchOption{
		dbus.WithMatchSender(service),
		dbus.WithMatchObjectPath(object),
		dbus.WithMatchInterface(iface),
	}
	if err := conn.AddMatchSignal(match...); err != nil {
		return fail("subscribe to notification signals", err)
	}
	signals := make(chan *dbus.Signal, eventBuf)
	conn.Signal(signals)
	return &dbusTransport{
		conn:    conn,
		object:  conn.Object(service, object),
		signals: signals,
		match:   match,
	}, nil
}

func (t *dbusTransport) Capabilities() ([]string, error) {
	var capabilities []string
	err := t.object.Call(iface+".GetCapabilities", 0).Store(&capabilities)
	return capabilities, err
}

func (t *dbusTransport) Notify(appName, icon, summary, body string, actions []string) (uint32, error) {
	var id uint32
	err := t.object.Call(iface+".Notify", 0,
		appName,
		uint32(0),
		icon,
		summary,
		body,
		actions,
		map[string]dbus.Variant{},
		expireSet,
	).Store(&id)
	return id, err
}

func (t *dbusTransport) CloseNotification(id uint32) error {
	return t.object.Call(iface+".CloseNotification", 0, id).Err
}

func (t *dbusTransport) Signals() <-chan *dbus.Signal { return t.signals }

func (t *dbusTransport) Close() error {
	removeErr := t.conn.RemoveMatchSignal(t.match...)
	closeErr := t.conn.Close()
	if removeErr != nil {
		return removeErr
	}
	return closeErr
}
