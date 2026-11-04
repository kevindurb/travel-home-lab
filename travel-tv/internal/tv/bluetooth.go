package tv

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Device is one Bluetooth controller as the TV settings page shows it.
type Device struct {
	Address   string `json:"address"`
	Name      string `json:"name"`
	Paired    bool   `json:"paired"`
	Trusted   bool   `json:"trusted"`
	Connected bool   `json:"connected"`
	// Battery is a percentage, or -1 when the device does not report one.
	Battery int `json:"battery"`
}

// BluetoothStatus is the JSON returned by GET /api/bluetooth.
type BluetoothStatus struct {
	Available   bool     `json:"available"`
	Powered     bool     `json:"powered"`
	Discovering bool     `json:"discovering"`
	Adapter     string   `json:"adapter,omitempty"`
	Devices     []Device `json:"devices"`
	Error       string   `json:"error,omitempty"`
}

// Bluetooth pairs and connects controllers through BlueZ. Implementations
// report unavailability instead of failing so the rest of the kiosk works
// when bluetoothd is missing (a dev container, a Pi without the package).
type Bluetooth interface {
	Status(ctx context.Context) BluetoothStatus
	Scan(ctx context.Context, duration time.Duration) error
	Pair(ctx context.Context, address string) error
	Connect(ctx context.Context, address string) error
	Disconnect(ctx context.Context, address string) error
	Forget(ctx context.Context, address string) error
}

// Bluez talks to bluetoothd over the system D-Bus socket.
type Bluez struct {
	mu              sync.Mutex
	agentRegistered bool
	scanTimer       *time.Timer
}

// NewBluetooth returns the BlueZ client. Nothing is dialled until the first
// request, so a missing socket is an "unavailable" status rather than a
// startup failure.
func NewBluetooth() *Bluez {
	return &Bluez{}
}

const (
	bluezService   = "org.bluez"
	bluezAgentPath = dbus.ObjectPath("/org/travellab/tv/bluetooth/agent")
	agentInterface = "org.bluez.Agent1"
)

// bluezAgent is the Just Works agent gamepads need: it accepts every request
// without a PIN. It is registered as NoInputNoOutput, so BlueZ only routes
// devices that do not require a passkey.
type bluezAgent struct{}

func (bluezAgent) Release() *dbus.Error { return nil }

func (bluezAgent) RequestPinCode(dbus.ObjectPath) (string, *dbus.Error) {
	return "0000", nil
}

func (bluezAgent) DisplayPinCode(dbus.ObjectPath, string) *dbus.Error { return nil }

func (bluezAgent) RequestPasskey(dbus.ObjectPath) (uint32, *dbus.Error) { return 0, nil }

func (bluezAgent) DisplayPasskey(dbus.ObjectPath, uint32, uint16) *dbus.Error { return nil }

func (bluezAgent) RequestConfirmation(dbus.ObjectPath, uint32) *dbus.Error { return nil }

func (bluezAgent) RequestAuthorization(dbus.ObjectPath) *dbus.Error { return nil }

func (bluezAgent) AuthorizeService(dbus.ObjectPath, string) *dbus.Error { return nil }

func (bluezAgent) Cancel() *dbus.Error { return nil }

// systemBus returns the shared system bus connection, registering the pairing
// agent once per process.
func (b *Bluez) systemBus() (*dbus.Conn, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.agentRegistered {
		return conn, nil
	}
	if err := conn.Export(bluezAgent{}, bluezAgentPath, agentInterface); err != nil {
		return nil, err
	}
	manager := conn.Object(bluezService, "/org/bluez")
	err = manager.Call("org.bluez.AgentManager1.RegisterAgent", 0, bluezAgentPath, "NoInputNoOutput").Err
	if err != nil && !isDBusError(err, "org.bluez.Error.AlreadyExists") {
		return nil, err
	}
	// Best effort: another agent may already be the default.
	_ = manager.Call("org.bluez.AgentManager1.RequestDefaultAgent", 0, bluezAgentPath).Err
	b.agentRegistered = true
	return conn, nil
}

type managedObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

func (b *Bluez) objects(ctx context.Context, conn *dbus.Conn) (managedObjects, error) {
	var objects managedObjects
	err := conn.Object(bluezService, "/").
		CallWithContext(ctx, "org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).
		Store(&objects)
	return objects, err
}

func adapterFrom(objects managedObjects) (dbus.ObjectPath, map[string]dbus.Variant) {
	for path, interfaces := range objects {
		if props, ok := interfaces["org.bluez.Adapter1"]; ok {
			return path, props
		}
	}
	return "", nil
}

func deviceFrom(objects managedObjects, address string) (dbus.ObjectPath, map[string]dbus.Variant, bool) {
	for path, interfaces := range objects {
		props, ok := interfaces["org.bluez.Device1"]
		if !ok {
			continue
		}
		if strings.EqualFold(stringProp(props, "Address"), address) {
			return path, props, true
		}
	}
	return "", nil, false
}

// Status reads the adapter and device tree, powering the adapter on if needed.
func (b *Bluez) Status(ctx context.Context) BluetoothStatus {
	status := BluetoothStatus{Devices: []Device{}}

	conn, err := b.systemBus()
	if err != nil {
		status.Error = err.Error()
		return status
	}

	objects, err := b.objects(ctx, conn)
	if err != nil {
		status.Error = err.Error()
		return status
	}

	adapter, props := adapterFrom(objects)
	if adapter == "" {
		status.Error = "no Bluetooth adapter"
		return status
	}

	if !boolProp(props, "Powered") {
		// A kiosk that has to be powered on by hand is not a kiosk. Best effort:
		// the UI still reports "off" when this fails.
		_ = setProperty(ctx, conn, adapter, "org.bluez.Adapter1", "Powered", true)
		if refreshed, err := b.objects(ctx, conn); err == nil {
			objects = refreshed
			_, props = adapterFrom(objects)
		}
	}

	status.Available = true
	status.Adapter = strings.TrimPrefix(string(adapter), "/org/bluez/")
	status.Powered = boolProp(props, "Powered")
	status.Discovering = boolProp(props, "Discovering")

	for _, interfaces := range objects {
		deviceProps, ok := interfaces["org.bluez.Device1"]
		if !ok {
			continue
		}
		address := stringProp(deviceProps, "Address")
		if address == "" {
			continue
		}
		device := Device{
			Address:   address,
			Name:      stringProp(deviceProps, "Alias"),
			Paired:    boolProp(deviceProps, "Paired"),
			Trusted:   boolProp(deviceProps, "Trusted"),
			Connected: boolProp(deviceProps, "Connected"),
			Battery:   -1,
		}
		if device.Name == "" {
			device.Name = stringProp(deviceProps, "Name")
		}
		if battery, ok := interfaces["org.bluez.Battery1"]; ok {
			device.Battery = int(numberProp(battery, "Percentage"))
		}
		status.Devices = append(status.Devices, device)
	}

	sort.SliceStable(status.Devices, func(i, j int) bool {
		a, b := status.Devices[i], status.Devices[j]
		if a.Connected != b.Connected {
			return a.Connected
		}
		if a.Paired != b.Paired {
			return a.Paired
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})

	return status
}

// Scan starts discovery and stops it after duration.
func (b *Bluez) Scan(ctx context.Context, duration time.Duration) error {
	if duration <= 0 || duration > 2*time.Minute {
		duration = 20 * time.Second
	}

	conn, adapter, err := b.adapter(ctx)
	if err != nil {
		return err
	}

	// "auto" asks for both classic and LE; pads are classic BR/EDR.
	filter := map[string]dbus.Variant{"Transport": dbus.MakeVariant("auto")}
	_ = conn.Object(bluezService, adapter).
		CallWithContext(ctx, "org.bluez.Adapter1.SetDiscoveryFilter", 0, filter).Err

	err = conn.Object(bluezService, adapter).
		CallWithContext(ctx, "org.bluez.Adapter1.StartDiscovery", 0).Err
	if err != nil && !isDBusError(err, "org.bluez.Error.InProgress") {
		return bluezError(err)
	}

	b.mu.Lock()
	if b.scanTimer != nil {
		b.scanTimer.Stop()
	}
	b.scanTimer = time.AfterFunc(duration, func() {
		_ = conn.Object(bluezService, adapter).
			Call("org.bluez.Adapter1.StopDiscovery", 0).Err
	})
	b.mu.Unlock()

	return nil
}

// Pair pairs, trusts and connects a device. Pairing blocks until the device
// answers, so the HTTP handler gives it a generous context.
func (b *Bluez) Pair(ctx context.Context, address string) error {
	conn, path, err := b.device(ctx, address)
	if err != nil {
		return err
	}

	if err := conn.Object(bluezService, path).
		CallWithContext(ctx, "org.bluez.Device1.Pair", 0).Err; err != nil {
		return bluezError(err)
	}

	// Trust so BlueZ reconnects the pad when it wakes up; that is the everyday
	// case, not the pairing.
	_ = setProperty(ctx, conn, path, "org.bluez.Device1", "Trusted", true)

	if err := conn.Object(bluezService, path).
		CallWithContext(ctx, "org.bluez.Device1.Connect", 0).Err; err != nil &&
		!isDBusError(err, "org.bluez.Error.AlreadyConnected") {
		return bluezError(err)
	}
	return nil
}

func (b *Bluez) Connect(ctx context.Context, address string) error {
	conn, path, err := b.device(ctx, address)
	if err != nil {
		return err
	}
	err = conn.Object(bluezService, path).
		CallWithContext(ctx, "org.bluez.Device1.Connect", 0).Err
	if err != nil && !isDBusError(err, "org.bluez.Error.AlreadyConnected") {
		return bluezError(err)
	}
	return nil
}

func (b *Bluez) Disconnect(ctx context.Context, address string) error {
	conn, path, err := b.device(ctx, address)
	if err != nil {
		return err
	}
	err = conn.Object(bluezService, path).
		CallWithContext(ctx, "org.bluez.Device1.Disconnect", 0).Err
	if err != nil && !isDBusError(err, "org.bluez.Error.NotConnected") {
		return bluezError(err)
	}
	return nil
}

func (b *Bluez) Forget(ctx context.Context, address string) error {
	conn, adapter, err := b.adapter(ctx)
	if err != nil {
		return err
	}
	objects, err := b.objects(ctx, conn)
	if err != nil {
		return err
	}
	path, _, ok := deviceFrom(objects, address)
	if !ok {
		return nil
	}
	if err := conn.Object(bluezService, adapter).
		CallWithContext(ctx, "org.bluez.Adapter1.RemoveDevice", 0, path).Err; err != nil {
		return bluezError(err)
	}
	return nil
}

func (b *Bluez) adapter(ctx context.Context) (*dbus.Conn, dbus.ObjectPath, error) {
	conn, err := b.systemBus()
	if err != nil {
		return nil, "", err
	}
	objects, err := b.objects(ctx, conn)
	if err != nil {
		return nil, "", err
	}
	adapter, _ := adapterFrom(objects)
	if adapter == "" {
		return nil, "", errBluetooth("no Bluetooth adapter")
	}
	return conn, adapter, nil
}

func (b *Bluez) device(ctx context.Context, address string) (*dbus.Conn, dbus.ObjectPath, error) {
	conn, err := b.systemBus()
	if err != nil {
		return nil, "", err
	}
	objects, err := b.objects(ctx, conn)
	if err != nil {
		return nil, "", err
	}
	path, _, ok := deviceFrom(objects, address)
	if !ok {
		return nil, "", errBluetooth("device not found; scan again")
	}
	return conn, path, nil
}

func setProperty(ctx context.Context, conn *dbus.Conn, path dbus.ObjectPath, iface, name string, value any) error {
	return conn.Object(bluezService, path).
		CallWithContext(ctx, "org.freedesktop.DBus.Properties.Set", 0,
			iface, name, dbus.MakeVariant(value)).Err
}

func boolProp(props map[string]dbus.Variant, name string) bool {
	value, ok := props[name]
	if !ok {
		return false
	}
	flag, ok := value.Value().(bool)
	return ok && flag
}

func stringProp(props map[string]dbus.Variant, name string) string {
	value, ok := props[name]
	if !ok {
		return ""
	}
	text, ok := value.Value().(string)
	return text
}

func numberProp(props map[string]dbus.Variant, name string) uint8 {
	value, ok := props[name]
	if !ok {
		return 0
	}
	number, ok := value.Value().(uint8)
	if !ok {
		return 0
	}
	return number
}

func isDBusError(err error, name string) bool {
	dbusErr, ok := err.(dbus.Error)
	return ok && dbusErr.Name == name
}

// bluezError turns the verbose D-Bus errors into something a TV can show.
func bluezError(err error) error {
	if err == nil {
		return nil
	}
	dbusErr, ok := err.(dbus.Error)
	if !ok {
		return err
	}
	switch dbusErr.Name {
	case "org.bluez.Error.AuthenticationFailed", "org.bluez.Error.AuthenticationCanceled":
		return errBluetooth("pairing was rejected; put the controller in pairing mode and try again")
	case "org.bluez.Error.AlreadyExists", "org.bluez.Error.InProgress":
		return errBluetooth("the controller is busy; try again in a moment")
	case "org.bluez.Error.Failed":
		if len(dbusErr.Body) > 0 {
			if text, ok := dbusErr.Body[0].(string); ok && text != "" {
				return errBluetooth(text)
			}
		}
	}
	return errBluetooth(strings.TrimPrefix(dbusErr.Name, "org.bluez.Error."))
}

// errBluetooth is a user-facing Bluetooth failure.
type errBluetooth string

func (e errBluetooth) Error() string { return string(e) }

// unavailableBluetooth stands in when the server was built without a client
// (and is what a missing system bus degrades to).
type unavailableBluetooth struct{}

func (unavailableBluetooth) Status(context.Context) BluetoothStatus {
	return BluetoothStatus{
		Devices: []Device{},
		Error:   "Bluetooth is not available on this kiosk",
	}
}

func (unavailableBluetooth) Scan(context.Context, time.Duration) error {
	return errBluetooth("Bluetooth is not available on this kiosk")
}

func (unavailableBluetooth) Pair(context.Context, string) error {
	return errBluetooth("Bluetooth is not available on this kiosk")
}

func (unavailableBluetooth) Connect(context.Context, string) error {
	return errBluetooth("Bluetooth is not available on this kiosk")
}

func (unavailableBluetooth) Disconnect(context.Context, string) error {
	return errBluetooth("Bluetooth is not available on this kiosk")
}

func (unavailableBluetooth) Forget(context.Context, string) error {
	return errBluetooth("Bluetooth is not available on this kiosk")
}
