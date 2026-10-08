package machines

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"golang.org/x/crypto/ssh"
)

type ProbeRequest struct {
	Address string `json:"address"`
}
type Identity struct {
	Address     string `json:"address"`
	Fingerprint string `json:"fingerprint"`
	KeyType     string `json:"keyType"`
}
type PairRequest struct {
	Name           string `json:"name"`
	Address        string `json:"address"`
	Username       string `json:"username"`
	OS             string `json:"os"`
	Fingerprint    string `json:"fingerprint"`
	PrivateKeyPath string `json:"privateKeyPath"`
}

var errProbeComplete = errors.New("identity captured")

func (m *Manager) Probe(ctx context.Context, actor string, p ProbeRequest) (Identity, error) {
	if err := validateAddress(p.Address); err != nil {
		return Identity{}, err
	}
	if err := m.authorize(ctx, actor, "probe", p.Address, policy.EffectRead, "Fetch unauthenticated SSH server identity; this does not establish trust"); err != nil {
		return Identity{}, err
	}
	ctx, cancel := operationContext(ctx, 10)
	defer cancel()
	result := Identity{Address: p.Address}
	_, err := dialSSH(ctx, p.Address, &ssh.ClientConfig{User: "orole-identity-probe", HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		result.Fingerprint = ssh.FingerprintSHA256(key)
		result.KeyType = key.Type()
		return errProbeComplete
	}})
	if result.Fingerprint == "" {
		return Identity{}, err
	}
	return result, nil
}
func pinnedCallback(encoded, fingerprint string) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if fingerprint == "" || ssh.FingerprintSHA256(key) != fingerprint {
			return ErrHostKeyChanged
		}
		if encoded != "" && base64.StdEncoding.EncodeToString(key.Marshal()) != encoded {
			return ErrHostKeyChanged
		}
		return nil
	}
}
func (m *Manager) Pair(ctx context.Context, actor string, p PairRequest) (Machine, error) {
	if err := validateAddress(p.Address); err != nil {
		return Machine{}, err
	}
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Username) == "" || !strings.HasPrefix(p.Fingerprint, "SHA256:") || (p.OS != "linux" && p.OS != "windows") {
		return Machine{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor, "pair", p.Address, policy.EffectDestructive, "Trust SSH identity "+p.Fingerprint+" and import the selected private key into Credential Manager"); err != nil {
		return Machine{}, err
	}
	// The frontend sends a path only. Secret material stays in the daemon and OS vault.
	f, err := os.Open(p.PrivateKeyPath)
	if err != nil {
		return Machine{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Machine{}, err
	}
	if !st.Mode().IsRegular() || st.Size() > 32<<10 {
		return Machine{}, fmt.Errorf("%w: expected a private key file of at most 32 KiB", ErrInvalid)
	}
	key := make([]byte, st.Size())
	if _, err = f.ReadAt(key, 0); err != nil {
		return Machine{}, err
	}
	defer clear(key)
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return Machine{}, errors.New("private key could not be loaded; use an unencrypted OpenSSH key protected by your Windows account")
	}
	ctx, cancel := operationContext(ctx, 20)
	defer cancel()
	var hostKey string
	cb := pinnedCallback("", p.Fingerprint)
	client, err := dialSSH(ctx, p.Address, &ssh.ClientConfig{User: p.Username, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: func(h string, a net.Addr, k ssh.PublicKey) error {
		if err := cb(h, a, k); err != nil {
			return err
		}
		hostKey = base64.StdEncoding.EncodeToString(k.Marshal())
		return nil
	}})
	if err != nil {
		return Machine{}, err
	}
	defer client.Close()
	id := uuid.NewString()
	if err = m.secrets.Set("machines/"+id+"/key", key); err != nil {
		return Machine{}, err
	}
	backend := "posix"
	if p.OS == "windows" {
		backend = "powershell"
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO machines(id,name,kind,os,transport,backend,address,username,fingerprint,host_key,created_at) VALUES(?,?,'remote',?,'ssh',?,?,?,?,?,?)`, id, p.Name, p.OS, backend, p.Address, p.Username, p.Fingerprint, hostKey, time.Now().UnixMilli())
	if err != nil {
		_ = m.secrets.Delete("machines/" + id + "/key")
		return Machine{}, err
	}
	return m.Get(ctx, id)
}
func dialSSH(ctx context.Context, address string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	c, ch, req, err := ssh.NewClientConn(conn, address, cfg)
	if err != nil {
		stop()
		_ = conn.Close()
		return nil, err
	}
	if ctx.Err() != nil {
		stop()
		_ = c.Close()
		return nil, ctx.Err()
	}
	// Keep cancellation hooked to the socket for the lifetime of this operation.
	client := ssh.NewClient(c, ch, req)
	go func() { _ = client.Wait(); stop() }()
	return client, nil
}
func (m *Manager) connect(ctx context.Context, v Machine) (*ssh.Client, error) {
	if v.Kind != "remote" || v.Transport != "ssh" || v.hostKey == "" {
		return nil, ErrDenied
	}
	key, err := m.secrets.Get("machines/" + v.ID + "/key")
	if err != nil {
		return nil, fmt.Errorf("machine key unavailable in credential store: %w", err)
	}
	defer clear(key)
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, errors.New("stored machine key is invalid; remove and pair the machine again")
	}
	return dialSSH(ctx, v.Address, &ssh.ClientConfig{User: v.Username, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: pinnedCallback(v.hostKey, v.Fingerprint)})
}
