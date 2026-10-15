package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// qmpMaxWait bounds every QMP exchange, whatever the caller's context says.
const qmpMaxWait = 5 * time.Second

// qmpClient is a QMP connection past the capabilities handshake. QEMU sends its greeting once per
// connection, so one client is made per connection and reused for later commands.
type qmpClient struct {
	conn net.Conn
	dec  *json.Decoder
}

// qmpConnect reads QEMU's greeting and negotiates capabilities. QEMU dispatches commands from its main
// loop, which only starts after the machine (fw_cfg files included) is built, so the reply also proves
// QEMU has finished reading its command-line files.
func qmpConnect(ctx context.Context, conn net.Conn) (*qmpClient, error) {
	defer qmpDeadline(ctx, conn)()
	dec := json.NewDecoder(conn)
	var greeting map[string]json.RawMessage
	if err := dec.Decode(&greeting); err != nil {
		return nil, fmt.Errorf("qmp greeting: %w", err)
	}
	if _, ok := greeting["QMP"]; !ok {
		return nil, errors.New("qmp: unexpected greeting")
	}
	c := &qmpClient{conn: conn, dec: dec}
	if err := c.send("qmp_capabilities"); err != nil {
		return nil, err
	}
	return c, nil
}

// execute runs one argument-less command and waits for its reply; asynchronous events are skipped.
func (c *qmpClient) execute(ctx context.Context, cmd string) error {
	defer qmpDeadline(ctx, c.conn)()
	return c.send(cmd)
}

func (c *qmpClient) send(cmd string) error {
	if _, err := fmt.Fprintf(c.conn, "{\"execute\":%q}\n", cmd); err != nil {
		return fmt.Errorf("qmp %s: %w", cmd, err)
	}
	if err := qmpReturn(c.dec); err != nil {
		return fmt.Errorf("qmp %s: %w", cmd, err)
	}
	return nil
}

// qmpDeadline sets conn's deadline to ctx's or qmpMaxWait, whichever is sooner, and returns the reset.
func qmpDeadline(ctx context.Context, conn net.Conn) func() {
	deadline := time.Now().Add(qmpMaxWait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	return func() { _ = conn.SetDeadline(time.Time{}) }
}

// qmpPowerdown performs the QMP handshake on conn and asks QEMU for an ACPI power-down; the guest's
// systemd treats it as the power button and powers off cleanly. It does not wait for QEMU to exit.
// Call it at most once per connection (QEMU sends its greeting once).
func qmpPowerdown(ctx context.Context, conn net.Conn) error {
	c, err := qmpConnect(ctx, conn)
	if err != nil {
		return err
	}
	return c.execute(ctx, "system_powerdown")
}

// qmpReturn reads messages until a command's return or error; asynchronous events are skipped.
func qmpReturn(dec *json.Decoder) error {
	for {
		var msg struct {
			Return json.RawMessage `json:"return"`
			Error  *struct {
				Class string `json:"class"`
				Desc  string `json:"desc"`
			} `json:"error"`
		}
		if err := dec.Decode(&msg); err != nil {
			return err
		}
		if msg.Error != nil {
			return fmt.Errorf("%s: %s", msg.Error.Class, msg.Error.Desc)
		}
		if msg.Return != nil {
			return nil
		}
	}
}
